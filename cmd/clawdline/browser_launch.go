package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/config"
)

// browserLaunch is `clawdline browser launch`: Clawdline's own Chromium, with
// the extension already loaded and its native messaging host registered in
// its own profile, so nothing is installed into the person's Chrome. The
// profile lives in the daemon's directory and keeps its sign-ins. The daemon's
// POST /v1/browser/launch does the same through internal/adapters/browserlaunch.
func browserLaunch(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("browser launch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	bin := fs.String("browser", os.Getenv("CLAWDLINE_CHROMIUM"), "Chromium binary (default: CLAWDLINE_CHROMIUM, then Playwright's Chromium)")
	if fs.Parse(args) != nil {
		return 2
	}
	port, err := daemonPort()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	started, err := browserlaunch.Launch(browserlaunch.Options{
		Dir: config.Dir(), Port: port, Browser: browserlaunch.FindChromium(*bin, os.Getenv),
	})
	if err != nil {
		fmt.Fprintln(errOut, "clawdline browser launch:", err)
		return 1
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"ok": true, "pid": started.PID, "browser": started.Browser,
		"profile": started.Profile, "extension_id": started.ExtensionID})
	return 0
}
