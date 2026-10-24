package main

// `clawdline cloud pair|devices|revoke|rotate` — the pairing half, from a
// terminal.
//
// **These four go through the running daemon**, unlike `cloud on`, `cloud off`
// and `cloud login`, which write files. The reason is that pairing has one
// live piece of state — the invitation this machine is currently waiting on —
// and
// two processes holding two of them means two codes on two screens and one
// person, who then uses the wrong one. The daemon owns that state; this is a
// terminal for it.
//
// It also means a rotation reaches the socket that is actually up. A CLI that
// rewrote the key file on its own would leave the daemon signing with a key the
// relay had stopped accepting, and the person would see it minutes later as
// `relay_unauthorized`.

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/devices"
	"github.com/sainteye/clawdline/internal/adapters/qrcode"
	"github.com/sainteye/clawdline/internal/config"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// cloudDaemonJSON does one request to the daemon and decodes its answer.
//
// A refusal is returned as an error carrying the daemon's own sentence, rather
// than a status code: every one of these routes already answers in words, and
// re-wording them here would make two vocabularies for one failure.
func cloudDaemonJSON(method, path string, body any, out any) error {
	req, err := daemonRequest(method, path, body)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return errors.New(refusalText(res))
	}
	if out == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// A copied pairing command can run in a shell carrying an obsolete test state
// directory and port. Use the installed daemon only when that directory has
// no token, the selected port is not serving, and its own token authenticates
// a read.
func cloudPairRequest(method, path string, body any) (*http.Request, error) {
	port, err := daemonPort()
	if err != nil {
		return nil, err
	}
	return cloudPairRequestFor(method, path, body, config.Load(), config.DefaultDir(), port, config.DefaultPort)
}

func cloudPairRequestFor(method, path string, body any, cfg config.Config, defaultDir string, port, installedPort int) (*http.Request, error) {
	req, err := daemonRequestWithConfig(method, path, body, cfg, port)
	if err == nil || cfg.Dir == defaultDir {
		return req, err
	}
	if _, statErr := os.Stat(filepath.Join(cfg.Dir, devices.LocalTokenFile)); !os.IsNotExist(statErr) {
		return nil, err
	}
	if port != installedPort {
		connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond)
		if dialErr == nil {
			connection.Close()
			return nil, err
		}
	}
	installed := config.Config{Dir: defaultDir}
	check, checkErr := daemonRequestWithConfig(http.MethodGet, "/v1/cloud/status", nil, installed, installedPort)
	if checkErr != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	answer, checkErr := client.Do(check)
	if checkErr != nil {
		return nil, err
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		return nil, err
	}
	return daemonRequestWithConfig(method, path, body, installed, installedPort)
}

func cloudPairJSON(method, path string, body any, out any) error {
	req, err := cloudPairRequest(method, path, body)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return errors.New(refusalText(res))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// cloudPairFlags is the command's flag set, kept apart so its help can be read
// without parsing (and exiting on) -h.
func cloudPairFlags() (*flag.FlagSet, *string, *string, *time.Duration) {
	fs := flag.NewFlagSet("cloud pair", flag.ExitOnError)
	scope := fs.String("scope", "", cliCopy("cloud", "pair.scope_help", "viewer access: readonly or operate; when omitted, a browser this machine already paired keeps its scope and only a never-paired browser gets operate"))
	offer := fs.String("offer", "", cliCopy("cloud", "pair.offer_help", "finish a pairing with the code the browser is showing"))
	wait := fs.Duration("wait", 10*time.Minute, cliCopy("cloud", "pair.wait_help", "how long to wait for a browser"))
	return fs, scope, offer, wait
}

// cloudPairCommand shows a pairing link and waits, or finishes a pairing from
// a code the person carried from their browser.
func cloudPairCommand(args []string) {
	fs, scope, offer, wait := cloudPairFlags()
	_ = fs.Parse(args)
	if *scope != "" && !adaptercloud.ValidViewerScope(*scope) {
		fail(errors.New(cliCopy("cloud", "grant.invalid_scope", "scope must be readonly or operate")))
	}

	if *offer != "" {
		var state cloudtransport.PairingState
		if err := cloudPairJSON(http.MethodPost, "/v1/cloud/pairing/offer",
			map[string]any{"offer": strings.TrimSpace(*offer), "scope": *scope}, &state); err != nil {
			fail(err)
		}
		printPairingState(state)
		return
	}

	var state cloudtransport.PairingState
	if err := cloudPairJSON(http.MethodPost, "/v1/cloud/pairing", map[string]any{"scope": *scope}, &state); err != nil {
		fail(err)
	}
	// The code first: on an iPhone the pairing has to happen inside the Home
	// Screen app, and a link opened from a message or a notification lands in
	// Safari, whose storage that app does not share. Scanning this code from
	// the app's "Scan the QR on the machine" keeps it in the app.
	if code, err := qrcode.Encode(state.Link); err == nil {
		fmt.Println(cliCopy("cloud", "pair.scan", "On an iPhone or iPad, open Clawdline from the Home Screen and scan this code:"))
		fmt.Println()
		fmt.Print(code.Terminal())
		fmt.Println()
	}
	fmt.Println(cliCopy("cloud", "pair.open_1", "Open this on the browser you want to pair, signed in to the same"))
	fmt.Println(cliCopy("cloud", "pair.open_2", "Clawdline Cloud account:"))
	fmt.Println()
	fmt.Println("   ", state.Link)
	fmt.Println()
	fmt.Printf(cliCopy("cloud", "pair.machine_key", "This machine's key is %s. The browser shows the same one when it finishes.\n"),
		state.MachineFingerprint)
	if state.ExpiresAt > 0 {
		// Local time with its zone, and the minutes left: a bare "3:43AM" read
		// on a server in UTC looked like the middle of the night elsewhere.
		until := time.Unix(state.ExpiresAt, 0)
		fmt.Printf(cliCopy("cloud", "pair.link_expires_in", "The link stops working at %s (in %d minutes).\n"),
			until.Local().Format("15:04 MST"), int(time.Until(until).Round(time.Minute)/time.Minute))
	}
	fmt.Println()
	fmt.Println(cliCopy("cloud", "pair.waiting", "Waiting… (Ctrl-C stops waiting; the link keeps working until it expires)"))

	deadline := time.Now().Add(*wait)
	for time.Now().Before(deadline) {
		time.Sleep(cloudtransport.PairingPollInterval)
		var now cloudtransport.PairingState
		if err := cloudPairJSON(http.MethodGet, "/v1/cloud/pairing", nil, &now); err != nil {
			fail(err)
		}
		switch now.Phase {
		case cloudtransport.PairingPaired, cloudtransport.PairingFailed:
			printPairingState(now)
			if now.Phase == cloudtransport.PairingFailed {
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintln(os.Stderr, cliCopy("cloud", "pair.timeout", "clawdline: nothing answered that link in time"))
	os.Exit(1)
}

func printPairingState(state cloudtransport.PairingState) {
	switch state.Phase {
	case cloudtransport.PairingPaired:
		fmt.Printf(cliCopy("cloud", "pair.paired", "paired     %s\n"), state.ViewerDeviceID)
		fmt.Printf(cliCopy("cloud", "pair.browser", "browser    %s\n"), state.ViewerFingerprint)
		fmt.Printf(cliCopy("cloud", "pair.machine", "machine    %s\n"), state.MachineFingerprint)
		if state.Scope == adaptercloud.ScopeReadOnly {
			fmt.Println(cliCopy("cloud", "pair.readonly", "           that browser may read; operating this machine is disabled"))
		} else {
			fmt.Println(cliCopy("cloud", "pair.access", "           that browser can now read and, if commands are on, drive this machine"))
		}
	case cloudtransport.PairingFailed:
		fmt.Fprintf(os.Stderr, cliCopy("cloud", "pair.failed", "clawdline: the pairing did not complete: %s\n"), state.Error)
	default:
		fmt.Printf(cliCopy("cloud", "pair.phase", "pairing    %s\n"), state.Phase)
	}
}

// cloudDevicesCommand lists who may speak to this machine, and where that trust
// came from.
func cloudDevicesCommand() {
	var status cloudtransport.Status
	if err := cloudDaemonJSON(http.MethodGet, "/v1/cloud/status", nil, &status); err != nil {
		fail(err)
	}
	if !status.PinnedReadable && status.PinnedError != "" {
		fmt.Fprintf(os.Stderr, "clawdline: %s\n", status.PinnedError)
		os.Exit(1)
	}
	if len(status.Devices) == 0 {
		fmt.Println(cliCopy("cloud", "devices.none", "no browser has been paired with this machine"))
		fmt.Println(cliCopy("cloud", "devices.pair_hint", "run `clawdline cloud pair` to show one a link"))
		return
	}
	accountOnly := false
	for _, device := range status.Devices {
		source := cliCopy("cloud", "devices.roster_only", "account only")
		switch {
		case device.Revoked:
			source = cliCopy("cloud", "devices.revoked_here", "revoked here")
		case device.Pinned:
			source = cliCopy("cloud", "devices.pinned_here", "paired here")
		default:
			accountOnly = true
		}
		if device.Pinned {
			source += " / " + device.Scope
		}
		name := device.Name
		if name == "" {
			name = device.Kind
		}
		fmt.Printf("%-24s %-20s %-13s %s\n", device.ID, device.Fingerprint, source, name)
		if len(device.Caps) > 0 {
			fmt.Printf("%-24s %s\n", "", strings.Join(device.Caps, " "))
		}
	}
	// A fresh machine lists every browser on the account; without this line
	// the ones it never paired read like strangers that got in.
	if accountOnly {
		fmt.Println()
		fmt.Println(cliCopy("cloud", "devices.account_only_legend",
			"account only: a browser on your Clawdline Cloud account that was paired with another of your machines, not this one. It is admitted here too; `clawdline cloud revoke <device-id>` refuses it on this machine."))
	}
}

// cloudRevokeCommand throws one browser out of this machine.
func cloudRevokeCommand(args []string) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, cliCopy("cloud", "revoke.usage", "usage: clawdline cloud revoke <device-id>"))
		os.Exit(2)
	}
	var answer struct {
		Device  string `json:"device"`
		Revoked bool   `json:"revoked"`
	}
	if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/devices/revoke",
		map[string]any{"device": strings.TrimSpace(args[0])}, &answer); err != nil {
		fail(err)
	}
	if !answer.Revoked {
		fmt.Printf(cliCopy("cloud", "revoke.no_change", "nothing changed: %s was not a browser this machine had pinned\n"), answer.Device)
		return
	}
	fmt.Printf(cliCopy("cloud", "revoke.revoked", "revoked    %s\n"), answer.Device)
	fmt.Println(cliCopy("cloud", "revoke.refusal", "           its next envelope is refused here, whatever the account's list says"))
}

// cloudRotateCommand replaces this machine's signing key.
//
// It asks first, by name. The cost is that every browser holding the old key
// stops being able to verify this machine, and a person who is not shown which
// browsers cannot weigh that.
func cloudRotateCommand(args []string) {
	fs := flag.NewFlagSet("cloud rotate", flag.ExitOnError)
	yes := fs.Bool("yes", false, cliCopy("cloud", "rotate.yes_help", "do not ask"))
	_ = fs.Parse(args)

	var preview struct {
		Repair []string `json:"repair"`
	}
	if err := cloudDaemonJSON(http.MethodGet, "/v1/cloud/keys/rotate", nil, &preview); err != nil {
		fail(err)
	}
	if len(preview.Repair) > 0 {
		fmt.Println(cliCopy("cloud", "rotate.warning_1", "Rotating this machine's signing key makes these browsers stop being able to"))
		fmt.Println(cliCopy("cloud", "rotate.warning_2", "verify this machine. Each one has to be paired again:"))
		for _, row := range preview.Repair {
			fmt.Println("   ", row)
		}
	} else {
		fmt.Println(cliCopy("cloud", "rotate.none", "No browser has been paired with this machine, so nothing has to be repaired."))
	}
	if !*yes {
		fmt.Print(cliCopy("cloud", "rotate.prompt", "Rotate anyway? [y/N] "))
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			fmt.Println(cliCopy("cloud", "rotate.cancelled", "nothing was rotated"))
			return
		}
	}
	var outcome cloudtransport.RotationOutcome
	if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/keys/rotate",
		map[string]any{"confirm": true}, &outcome); err != nil {
		fail(err)
	}
	fmt.Printf(cliCopy("cloud", "rotate.rotated", "rotated    %s (was %s)\n"), outcome.Fingerprint, outcome.Previous)
	fmt.Printf(cliCopy("cloud", "rotate.epoch", "epoch      key %d, identity %d\n"), outcome.KeyEpoch, outcome.IdentityEpoch)
	if outcome.Reconnected {
		fmt.Println(cliCopy("cloud", "rotate.reconnecting", "           the line went down and is coming back on the new key"))
	}
}

func cloudGrantCommand(args []string) {
	if len(args) != 2 || !adaptercloud.ValidViewerScope(args[1]) {
		fail(errors.New(cliCopy("cloud", "grant.usage", "usage: clawdline cloud grant <device-id> <readonly|operate>")))
	}
	var answer struct {
		Device string `json:"device"`
		Scope  string `json:"scope"`
	}
	if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/devices/grant", map[string]any{"device": args[0], "scope": args[1]}, &answer); err != nil {
		fail(err)
	}
	fmt.Printf(cliCopy("cloud", "grant.changed", "viewer %s: %s\n"), answer.Device, answer.Scope)
}
