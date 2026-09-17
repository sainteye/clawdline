package main

// `clawdline cloud pair|devices|revoke|rotate` — the pairing half, from a
// terminal.
//
// **These four go through the running daemon**, unlike `cloud on`, `cloud off`
// and `cloud login`, which write files. The reason is that pairing has one
// live piece of state — the invitation this Mac is currently waiting on — and
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
	"net/http"
	"os"
	"strings"
	"time"

	cloudtransport "github.com/sainteye/clawdline-go/internal/transport/cloud"
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

// cloudPairCommand shows a pairing link and waits, or finishes a pairing from
// a code the person carried from their browser.
func cloudPairCommand(args []string) {
	fs := flag.NewFlagSet("cloud pair", flag.ExitOnError)
	offer := fs.String("offer", "", "finish a pairing with the code the browser is showing")
	wait := fs.Duration("wait", 10*time.Minute, "how long to wait for a browser")
	_ = fs.Parse(args)

	if *offer != "" {
		var state cloudtransport.PairingState
		if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/pairing/offer",
			map[string]any{"offer": strings.TrimSpace(*offer)}, &state); err != nil {
			fail(err)
		}
		printPairingState(state)
		return
	}

	var state cloudtransport.PairingState
	if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/pairing", nil, &state); err != nil {
		fail(err)
	}
	fmt.Println("Open this on the browser you want to pair, signed in to the same")
	fmt.Println("Clawdline Cloud account:")
	fmt.Println()
	fmt.Println("   ", state.Link)
	fmt.Println()
	fmt.Printf("This Mac's key is %s. The browser shows the same one when it finishes.\n",
		state.MachineFingerprint)
	if state.ExpiresAt > 0 {
		fmt.Printf("The link stops working at %s.\n", time.Unix(state.ExpiresAt, 0).Format(time.Kitchen))
	}
	fmt.Println()
	fmt.Println("Waiting… (Ctrl-C stops waiting; the link keeps working until it expires)")

	deadline := time.Now().Add(*wait)
	for time.Now().Before(deadline) {
		time.Sleep(cloudtransport.PairingPollInterval)
		var now cloudtransport.PairingState
		if err := cloudDaemonJSON(http.MethodGet, "/v1/cloud/pairing", nil, &now); err != nil {
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
	fmt.Fprintln(os.Stderr, "clawdline: nothing answered that link in time")
	os.Exit(1)
}

func printPairingState(state cloudtransport.PairingState) {
	switch state.Phase {
	case cloudtransport.PairingPaired:
		fmt.Printf("paired     %s\n", state.ViewerDeviceID)
		fmt.Printf("browser    %s\n", state.ViewerFingerprint)
		fmt.Printf("this Mac   %s\n", state.MachineFingerprint)
		fmt.Println("           that browser can now read and, if commands are on, drive this Mac")
	case cloudtransport.PairingFailed:
		fmt.Fprintf(os.Stderr, "clawdline: the pairing did not complete: %s\n", state.Error)
	default:
		fmt.Printf("pairing    %s\n", state.Phase)
	}
}

// cloudDevicesCommand lists who may speak to this Mac, and where that trust
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
		fmt.Println("no browser has been paired with this Mac")
		fmt.Println("run `clawdline cloud pair` to show one a link")
		return
	}
	for _, device := range status.Devices {
		source := "roster only"
		switch {
		case device.Revoked:
			source = "revoked here"
		case device.Pinned:
			source = "pinned here"
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
}

// cloudRevokeCommand throws one browser out of this Mac.
func cloudRevokeCommand(args []string) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, "usage: clawdline cloud revoke <device-id>")
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
		fmt.Printf("nothing changed: %s was not a browser this Mac had pinned\n", answer.Device)
		return
	}
	fmt.Printf("revoked    %s\n", answer.Device)
	fmt.Println("           its next envelope is refused here, whatever the account's list says")
}

// cloudRotateCommand replaces this machine's signing key.
//
// It asks first, by name. The cost is that every browser holding the old key
// stops being able to verify this Mac, and a person who is not shown which
// browsers cannot weigh that.
func cloudRotateCommand(args []string) {
	fs := flag.NewFlagSet("cloud rotate", flag.ExitOnError)
	yes := fs.Bool("yes", false, "do not ask")
	_ = fs.Parse(args)

	var preview struct {
		Repair []string `json:"repair"`
	}
	if err := cloudDaemonJSON(http.MethodGet, "/v1/cloud/keys/rotate", nil, &preview); err != nil {
		fail(err)
	}
	if len(preview.Repair) > 0 {
		fmt.Println("Rotating this Mac's signing key makes these browsers stop being able to")
		fmt.Println("verify it. Each one has to be paired again:")
		for _, row := range preview.Repair {
			fmt.Println("   ", row)
		}
	} else {
		fmt.Println("No browser has been paired with this Mac, so nothing has to be repaired.")
	}
	if !*yes {
		fmt.Print("Rotate anyway? [y/N] ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			fmt.Println("nothing was rotated")
			return
		}
	}
	var outcome cloudtransport.RotationOutcome
	if err := cloudDaemonJSON(http.MethodPost, "/v1/cloud/keys/rotate",
		map[string]any{"confirm": true}, &outcome); err != nil {
		fail(err)
	}
	fmt.Printf("rotated    %s (was %s)\n", outcome.Fingerprint, outcome.Previous)
	fmt.Printf("epoch      key %d, identity %d\n", outcome.KeyEpoch, outcome.IdentityEpoch)
	if outcome.Reconnected {
		fmt.Println("           the line went down and is coming back on the new key")
	}
}
