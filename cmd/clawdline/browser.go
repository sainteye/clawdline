package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/config"
)

// browserCommand is `clawdline browser <op>`: an agent's use of the person's
// Chrome through the daemon and the Clawdline extension (docs/browser.md).
func browserCommand(args []string) {
	os.Exit(runBrowser(args, os.Getenv, os.Stdout, os.Stderr))
}

const browserUsage = `usage: clawdline browser <command>
  status                         whether the extension is connected and browser use is on
  launch [--browser PATH]        start Clawdline's own Chromium with the extension already in it
  open <url>                     open a new tab for this Session
  tabs                           list the tabs this Session may use
  read [--tab N]                 the page's text and the elements that can be pressed or filled
  click <text> | --x N --y N     press the element whose label is <text>, or a point
  type <text> | --stdin          type into the focused field (Chinese included); --stdin reads a
                                 Secret Handoff: clawdline secret run <note> -- clawdline browser type --stdin
  key <name>                     Enter, Tab, Backspace, Escape, ArrowDown, ArrowUp, PageDown, PageUp
  scroll <down|up>               scroll the page by about one screen
  screenshot [--out FILE]        save a JPEG of the tab and print its path
  release                        stop using this Session's tab (the tab stays open)
  stop                           stop all browser use until the person resumes it
  audit [--limit N]              what this Session did in the browser`

func runBrowser(args []string, getenv func(string) string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(out, browserUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] == "install-host" {
		return browserInstallHost(args[1:], out, errOut)
	}
	if args[0] == "launch" {
		return browserLaunch(args[1:], out, errOut)
	}
	b, err := openBroker(0)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	op, rest := args[0], args[1:]
	switch op {
	case "status":
		a, err := b.request(http.MethodGet, "/v1/browser/status", nil, nil, "")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return report(out, errOut, "browser status", a)
	case "stop":
		a, err := b.request(http.MethodPost, "/v1/browser/stop", nil, map[string]bool{"stopped": true}, "")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return report(out, errOut, "browser stop", a)
	}
	conversation, _, err := conversationFromEnv(getenv)
	if err != nil || conversation == "" {
		fmt.Fprintln(errOut, "clawdline browser: run this from a Clawdline Session; no live conversation was found in the environment")
		return 2
	}
	if op == "audit" {
		fs := flag.NewFlagSet("browser audit", flag.ContinueOnError)
		fs.SetOutput(errOut)
		limit := fs.String("limit", "50", "rows")
		if fs.Parse(rest) != nil {
			return 2
		}
		a, err := b.request(http.MethodGet, "/v1/browser/audit", map[string][]string{"conversation": {conversation}, "limit": {*limit}}, nil, "")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return report(out, errOut, "browser audit", a)
	}
	cmdArgs, outFile, code := browserArgs(op, rest, os.Stdin, errOut)
	if code != 0 {
		return code
	}
	a, err := b.request(http.MethodPost, "/v1/browser/commands", nil, map[string]any{"conversation": conversation, "op": op, "args": cmdArgs}, "")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if op == "screenshot" && a.ok() {
		return saveScreenshot(a, outFile, out, errOut)
	}
	return report(out, errOut, "browser "+op, a)
}

// browserArgs turns the words after an op into the command's JSON arguments.
func browserArgs(op string, rest []string, stdin io.Reader, errOut io.Writer) (map[string]any, string, int) {
	fs := flag.NewFlagSet("browser "+op, flag.ContinueOnError)
	fs.SetOutput(errOut)
	tab := fs.Int("tab", 0, "tab id")
	x := fs.Float64("x", -1, "x")
	y := fs.Float64("y", -1, "y")
	outFile := fs.String("out", "", "file")
	fromStdin := fs.Bool("stdin", false, "read the text from standard input")
	if fs.Parse(rest) != nil {
		return nil, "", 2
	}
	words := strings.Join(fs.Args(), " ")
	args := map[string]any{}
	if *tab > 0 {
		args["tab"] = *tab
	}
	need := func(name string) bool {
		if strings.TrimSpace(words) == "" {
			fmt.Fprintf(errOut, "clawdline browser %s: %s is required\n", op, name)
			return false
		}
		return true
	}
	switch op {
	case "open":
		if !need("a URL") {
			return nil, "", 2
		}
		args["url"] = words
	case "click":
		if *x >= 0 && *y >= 0 {
			args["x"], args["y"] = *x, *y
		} else if !need("the element's text, or --x and --y") {
			return nil, "", 2
		} else {
			args["target"] = words
		}
	case "type":
		if *fromStdin {
			// A secret arrives here from `clawdline secret run`: it is typed,
			// never printed, and the audit keeps only its length.
			data, err := io.ReadAll(io.LimitReader(stdin, maxStdinText+1))
			if err != nil || len(data) > maxStdinText || len(data) == 0 {
				fmt.Fprintln(errOut, "clawdline browser type --stdin: standard input was empty, unreadable or larger than 16 KiB")
				return nil, "", 2
			}
			text := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
			args["text"], args["secret"] = text, true
			break
		}
		if !need("the text") {
			return nil, "", 2
		}
		args["text"] = words
	case "key":
		if !need("a key name") {
			return nil, "", 2
		}
		args["key"] = words
	case "scroll":
		if words == "" {
			words = "down"
		}
		if words != "down" && words != "up" {
			fmt.Fprintln(errOut, "clawdline browser scroll: down or up")
			return nil, "", 2
		}
		args["direction"] = words
	case "tabs", "read", "screenshot", "release":
	default:
		fmt.Fprintln(errOut, browserUsage)
		return nil, "", 2
	}
	return args, *outFile, 0
}

func saveScreenshot(a answer, outFile string, out, errOut io.Writer) int {
	var body struct {
		Value struct {
			Data string `json:"data"`
			URL  string `json:"url"`
		} `json:"value"`
	}
	if json.Unmarshal(a.Body, &body) != nil || body.Value.Data == "" {
		fmt.Fprintln(errOut, "clawdline browser screenshot: the extension returned no image")
		return 1
	}
	data, err := base64.StdEncoding.DecodeString(body.Value.Data)
	if err != nil {
		fmt.Fprintln(errOut, "clawdline browser screenshot: the image was not valid base64")
		return 1
	}
	if outFile == "" {
		f, err := os.CreateTemp("", "clawdline-browser-*.jpg")
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		outFile = f.Name()
		f.Close()
	}
	if err := os.WriteFile(outFile, data, 0o600); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"ok": true, "path": outFile, "url": body.Value.URL, "bytes": len(data)})
	return 0
}

// maxStdinText bounds what `type --stdin` reads, within the bridge's own
// argument bound.
const maxStdinText = 16 << 10

// ---- native messaging host ----

// nativeMessageLimit is Chrome's bound on one message from a host to an
// extension (1 MiB). Results the other way are bounded by the daemon.
const nativeMessageLimit = 1 << 20

// browserHostCommand is `clawdline browser-host`, which Chrome starts for the
// Clawdline extension. It relays commands from the daemon's long poll to the
// extension on stdout, and the extension's results from stdin to the daemon.
func browserHostCommand() {
	os.Exit(runBrowserHost(os.Stdin, os.Stdout, os.Stderr))
}

func runBrowserHost(in io.Reader, out io.Writer, errOut io.Writer) int {
	b, err := openBroker(0)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	b.client = &http.Client{Timeout: 45 * time.Second}
	var writeMu sync.Mutex
	send := func(v any) error {
		data, err := json.Marshal(v)
		if err != nil || len(data) > nativeMessageLimit {
			return errors.New("browser-host: message too large for the extension")
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := binary.Write(out, binary.LittleEndian, uint32(len(data))); err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	}
	var infoMu sync.Mutex
	info := map[string]string{"version": "unknown"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := bufio.NewReader(in)
		for {
			msg, err := readNativeMessage(r)
			if err != nil {
				return // Chrome closed the port.
			}
			var m struct {
				Hello   map[string]string `json:"hello"`
				Results json.RawMessage   `json:"results"`
			}
			if json.Unmarshal(msg, &m) != nil {
				continue
			}
			if m.Hello != nil {
				infoMu.Lock()
				for k, v := range m.Hello {
					if k == "version" || k == "browser" || k == "profile" {
						info[k] = v
					}
				}
				infoMu.Unlock()
			}
			if len(m.Results) > 0 {
				if _, err := b.request(http.MethodPost, "/v1/browser/host/results", nil, json.RawMessage(`{"results":`+string(m.Results)+`}`), ""); err != nil {
					fmt.Fprintln(errOut, err)
				}
			}
		}
	}()
	for {
		select {
		case <-done:
			return 0
		default:
		}
		infoMu.Lock()
		body := map[string]string{}
		for k, v := range info {
			body[k] = v
		}
		infoMu.Unlock()
		a, err := b.request(http.MethodPost, "/v1/browser/host/poll", nil, body, "")
		if err != nil || !a.ok() {
			_ = send(map[string]any{"daemon": "unreachable"})
			select {
			case <-done:
				return 0
			case <-time.After(3 * time.Second):
			}
			continue
		}
		var polled struct {
			Commands []json.RawMessage `json:"commands"`
		}
		if json.Unmarshal(a.Body, &polled) != nil {
			continue
		}
		_ = send(map[string]any{"daemon": "connected"})
		for _, c := range polled.Commands {
			if err := send(map[string]json.RawMessage{"command": c}); err != nil {
				fmt.Fprintln(errOut, err)
			}
		}
	}
}

func readNativeMessage(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n > 64<<20 {
		return nil, errors.New("browser-host: message too large")
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

// ---- installing the host for one Chrome profile directory ----

// browserInstallHost writes Chrome's native messaging manifest for the
// extension into a Chrome user-data directory, and a launcher that pins this
// daemon's directory and port (internal/adapters/browserlaunch).
func browserInstallHost(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("browser install-host", flag.ContinueOnError)
	fs.SetOutput(errOut)
	extID := fs.String("extension-id", "", "the extension's id")
	chromeDir := fs.String("chrome-dir", "", "Chrome's user-data directory")
	if fs.Parse(args) != nil {
		return 2
	}
	if !browserlaunch.ValidExtensionID(*extID) || *chromeDir == "" || !filepath.IsAbs(*chromeDir) {
		fmt.Fprintln(errOut, "usage: clawdline browser install-host --extension-id <32 letters a-p> --chrome-dir <absolute path>")
		return 2
	}
	dir := config.Dir()
	exe, err := browserlaunch.LauncherBinary(dir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	port, err := daemonPort()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	host, err := browserlaunch.InstallHost(dir, port, exe, *extID, *chromeDir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"ok": true, "manifest": host.Manifest, "launcher": host.Launcher})
	return 0
}
