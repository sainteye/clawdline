package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// The two commands that make pairing and signing in work without the macOS
// shell: `open` gives a browser on this machine a device of its own, and
// `pair` shows a pairing code in the terminal, where a Linux or Windows machine
// has nowhere else to show it.
//
// Both talk to the running daemon with this machine's own token, read from
// this app's directory and never from the Swift app's.

// daemonPort is the port the daemon listens on: CLAWDLINE_NEXT_PORT, else 7727.
func daemonPort() (int, error) {
	v := os.Getenv("CLAWDLINE_NEXT_PORT")
	if v == "" {
		return config.DefaultPort, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("CLAWDLINE_NEXT_PORT is not a port: %q", v)
	}
	return port, nil
}

// localToken reads this machine's token, which the daemon wrote when it
// started.
func localToken(cfg config.Config) (string, error) {
	path := filepath.Join(cfg.Dir, devices.LocalTokenFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no local token at %s — is the daemon running? (clawdline serve)", path)
	}
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

func daemonRequest(method, path string, body any) (*http.Request, error) {
	cfg := config.Load()
	port, err := daemonPort()
	if err != nil {
		return nil, err
	}
	token, err := localToken(cfg)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// refusalText is the message inside the Swift envelope, or the status.
func refusalText(res *http.Response) string {
	var refusal contract.AuthRefusal
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if json.Unmarshal(data, &refusal) == nil && refusal.Error.Message != "" {
		return fmt.Sprintf("%s (%s)", refusal.Error.Message, refusal.Error.Code)
	}
	return res.Status
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "clawdline:", err)
	os.Exit(1)
}

// openCommand is the Swift app's "Open in a browser": a device of its own
// named "Browser on this machine", handed over in the address. In the fragment,
// not the query — a browser never sends a fragment and never logs it.
func openCommand(args []string) {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	send := fs.Bool("send", false, "let this browser type into sessions as well as read them")
	printOnly := fs.Bool("print", false, "print the address instead of opening it (it carries a key)")
	_ = fs.Parse(args)

	req, err := daemonRequest(http.MethodPost, "/v1/auth/devices/browser", contract.BrowserRequest{Send: *send})
	if err != nil {
		fail(err)
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		fail(fmt.Errorf("the daemon did not answer: %w", err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(errors.New(refusalText(res)))
	}
	var made contract.BrowserDevice
	if err := json.NewDecoder(res.Body).Decode(&made); err != nil {
		fail(fmt.Errorf("the daemon's answer was not a device: %w", err))
	}
	if *printOnly {
		fmt.Println(made.URL)
		return
	}
	if err := openURL(made.URL); err != nil {
		fail(fmt.Errorf("could not open a browser (%v); run `clawdline open --print` and open the address yourself", err))
	}
	fmt.Printf("opened a browser as device %s\n", made.ID)
}

func openURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Run()
}

// pairCommand prints each pairing code as it is asked for. Without --watch it
// waits for one and exits.
//
// The words are the Swift app's alert (Copy+English.swift pairingAsks and
// pairingCode), in English like the rest of this command line.
func pairCommand(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	watch := fs.Bool("watch", false, "keep printing codes until interrupted")
	_ = fs.Parse(args)

	req, err := daemonRequest(http.MethodGet, "/v1/auth/pairings", nil)
	if err != nil {
		fail(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(fmt.Errorf("the daemon did not answer: %w", err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(errors.New(refusalText(res)))
	}
	if *watch {
		fmt.Fprintln(os.Stderr, "waiting for a device to ask to pair (Ctrl-C to stop)")
	} else {
		fmt.Fprintln(os.Stderr, "waiting for a device to ask to pair")
	}

	reader := bufio.NewReader(res.Body)
	event, data := "", ""
	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "" && event != "":
			if event == "pairing" {
				var n contract.PairingNotice
				if json.Unmarshal([]byte(data), &n) == nil {
					printPairing(n)
					if !*watch {
						return
					}
				}
			}
			event, data = "", ""
		}
		if err != nil {
			if *watch || err != io.EOF {
				fail(fmt.Errorf("the pairing stream ended: %v", err))
			}
			fail(errors.New("the pairing stream ended before anybody asked"))
		}
	}
}

func printPairing(n contract.PairingNotice) {
	fmt.Printf("\n%s wants to pair with this machine\n\n", n.Name)
	fmt.Printf("Type this code into it:\n\n%s\n\n", n.Code)
	fmt.Printf("It is good for two minutes. If you did not just ask for this, ignore it — "+
		"whoever asked cannot finish without this code.\n(expires %s)\n",
		time.Unix(n.Expires, 0).Format("15:04:05"))
}
