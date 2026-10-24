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
	"github.com/sainteye/clawdline/internal/adapters/install"
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

// daemonPort is the port the daemon listens on: CLAWDLINE_NEXT_PORT, else the
// port `clawdline setup --port` recorded in this state directory's
// service.json, else 7727. Without the middle step every command after a
// `--port` install knocked on 7727 and was refused by whatever answered there.
func daemonPort() (int, error) {
	v := os.Getenv("CLAWDLINE_NEXT_PORT")
	if v == "" {
		if sf, err := install.ReadServiceFile(config.Load().Dir); err == nil && sf.Port > 0 && sf.Port <= 65535 {
			return sf.Port, nil
		}
		return config.DefaultPort, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf(cliCopy("core", "auth.daemon_port_invalid", "CLAWDLINE_NEXT_PORT is not a port: %q"), v)
	}
	return port, nil
}

// localToken reads this machine's token, which the daemon wrote when it
// started.
func localToken(cfg config.Config) (string, error) {
	path := filepath.Join(cfg.Dir, devices.LocalTokenFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf(cliCopy("core", "auth.local_token_missing", "no local token at %s — is the daemon running? (clawdline serve)"), path)
	}
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf(cliCopy("core", "auth.local_token_empty", "%s is empty"), path)
	}
	return token, nil
}

func daemonRequest(method, path string, body any) (*http.Request, error) {
	cfg := config.Load()
	port, err := daemonPort()
	if err != nil {
		return nil, err
	}
	return daemonRequestWithConfig(method, path, body, cfg, port)
}

func daemonRequestWithConfig(method, path string, body any, cfg config.Config, port int) (*http.Request, error) {
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
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if refusal, ok := parseCLIHTTPRefusal(data); ok {
		return fmt.Sprintf("%s (%s)", refusal.humanDetail(currentCLILanguage()), refusal.Code)
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
	printOnly, port, err := parseOpenArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fail(err)
	}

	req, err := daemonRequestWithConfig(http.MethodPost, "/v1/auth/devices/browser", contract.BrowserRequest{}, config.Load(), port)
	if err != nil {
		fail(err)
	}
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		fail(fmt.Errorf(cliCopy("core", "auth.daemon_unreachable", "the daemon did not answer: %w"), err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(errors.New(refusalText(res)))
	}
	var made contract.BrowserDevice
	if err := json.NewDecoder(res.Body).Decode(&made); err != nil {
		fail(fmt.Errorf(cliCopy("core", "auth.device_unreadable", "the daemon's answer was not a device: %w"), err))
	}
	if printOnly {
		fmt.Println(made.URL)
		return
	}
	if err := openURL(made.URL); err != nil {
		fail(fmt.Errorf(cliCopy("core", "auth.browser_open_failed", "could not open a browser (%v); run `clawdline open --print` and open the address yourself"), err))
	}
	fmt.Printf(cliCopy("core", "auth.opened_browser", "opened a browser as device %s\n"), made.ID)
}

// parseOpenArgs reads `open`'s flags. --port is the one `clawdline setup`
// prints for an install on a port other than 7727 ("Not in front of you? Run
// clawdline open --port 7790"); without it that line answered "flag provided
// but not defined: -port". With no --port the port is CLAWDLINE_NEXT_PORT,
// else 7727, as before.
func parseOpenArgs(args []string) (printOnly bool, port int, err error) {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	wantPrint := fs.Bool("print", false, "print the address instead of opening it (it carries a key)")
	named := fs.Int("port", 0, cliCopy("item", "flag.port", "the daemon's port (default CLAWDLINE_NEXT_PORT, else 7727)"))
	if err := fs.Parse(args); err != nil {
		return false, 0, err
	}
	if fs.NArg() > 0 {
		return false, 0, fmt.Errorf("open takes no arguments, only --print and --port; got %q", fs.Arg(0))
	}
	if *named == 0 {
		port, err := daemonPort()
		return *wantPrint, port, err
	}
	if *named < 1 || *named > 65535 {
		return false, 0, fmt.Errorf("--port %d is not a port", *named)
	}
	return *wantPrint, *named, nil
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
// pairingCode); the device name and pairing code remain as received.
func pairCommand(args []string) {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	permission := fs.String("permission", "", cliCopy("ops", "devices.pair_permission_help", "select read-only or read-and-operate on this machine before showing the pairing code"))
	watch := fs.Bool("watch", false, cliCopy("core", "auth.watch_help", "keep printing codes until interrupted"))
	_ = fs.Parse(args)
	if *permission != "" && *permission != "read-only" && *permission != "read-and-operate" {
		fail(errors.New(cliCopy("ops", "devices.permission_invalid", "permission must be read-only or read-and-operate")))
	}

	req, err := daemonRequest(http.MethodGet, "/v1/auth/pairings", nil)
	if err != nil {
		fail(err)
	}
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(fmt.Errorf(cliCopy("core", "auth.daemon_unreachable", "the daemon did not answer: %w"), err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fail(errors.New(refusalText(res)))
	}
	if *watch {
		fmt.Fprintln(os.Stderr, cliCopy("core", "auth.wait_watch", "waiting for a device to ask to pair (Ctrl-C to stop)"))
	} else {
		fmt.Fprintln(os.Stderr, cliCopy("core", "auth.wait", "waiting for a device to ask to pair"))
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
					if *permission != "" {
						door, err := openLocalDoor()
						if err != nil {
							fail(err)
						}
						var result contract.AuthOK
						if err := door.doBody(http.MethodPost, "/v1/auth/pairings/"+n.PairingID+"/permission", contract.DevicePermission{Permission: *permission}, &result); err != nil {
							fail(err)
						}
					}
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
				fail(fmt.Errorf(cliCopy("core", "auth.pairing_stream_ended", "the pairing stream ended: %v"), err))
			}
			fail(errors.New(cliCopy("core", "auth.pairing_stream_no_request", "the pairing stream ended before anybody asked")))
		}
	}
}

func printPairing(n contract.PairingNotice) {
	printPairingTo(os.Stdout, n)
}

func printPairingTo(w io.Writer, n contract.PairingNotice) {
	fmt.Fprintf(w, cliCopy("core", "auth.pair_wants", "\n%s wants to pair with this machine\n\n"), n.Name)
	fmt.Fprintf(w, cliCopy("core", "auth.pair_code", "Type this code into it:\n\n%s\n\n"), n.Code)
	fmt.Fprintf(w, cliCopy("core", "auth.pair_expires", "It is good for two minutes. If you did not just ask for this, ignore it — "+
		"whoever asked cannot finish without this code.\n(expires %s)\n"),
		time.Unix(n.Expires, 0).Format("15:04:05"))
}
