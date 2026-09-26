package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/contract"
)

// `clawdline devices` is who may come in through this machine's own door: the
// browsers `clawdline open` signed in, the devices that paired with a code,
// and the ones that signed in with the password. `clawdline cloud devices` is
// a different list — the browsers Clawdline Cloud relays for — and neither
// command touches the other's.
//
// Both halves need this machine's own token, which is the only one the daemon
// lets list or revoke. That token is not in the list and cannot be revoked
// here, so this command cannot take away the key it is holding.

// localDoor is the daemon, asked with this machine's own token.
type localDoor struct {
	base   string
	token  string
	client *http.Client
}

func openLocalDoor() (localDoor, error) {
	port, err := daemonPort()
	if err != nil {
		return localDoor{}, err
	}
	token, err := localToken(config.Load())
	if err != nil {
		return localDoor{}, err
	}
	return localDoor{
		base:   fmt.Sprintf("http://127.0.0.1:%d", port),
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (d localDoor) do(method, path string, out any) error {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	req, err := http.NewRequest(method, d.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("the daemon did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New(refusalText(res))
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("the daemon's answer could not be read: %w", err)
	}
	return nil
}

func devicesUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: clawdline devices [--json]")
	fmt.Fprintln(w, "       clawdline devices revoke [--yes] <device-id>")
	fmt.Fprintln(w, "  the browsers and devices signed in to this machine directly (clawdline open, pairing, password);")
	fmt.Fprintln(w, "  browsers reached through Clawdline Cloud are `clawdline cloud devices`")
}

func devicesCommand(args []string) {
	door, err := openLocalDoor()
	if err != nil {
		fail(err)
	}
	if len(args) > 0 && args[0] == "revoke" {
		os.Exit(devicesRevoke(os.Stdout, os.Stderr, os.Stdin, door, args[1:]))
	}
	os.Exit(devicesList(os.Stdout, os.Stderr, door, args))
}

// devicesList prints one line per device, oldest first, and says what is not
// in the list: the key this command itself is holding.
func devicesList(out, errs io.Writer, door localDoor, args []string) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(errs)
	asJSON := fs.Bool("json", false, "print the daemon's answer as it is")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		devicesUsage(errs)
		return 2
	}
	var list contract.DeviceList
	if err := door.do(http.MethodGet, "/v1/auth/devices", &list); err != nil {
		fmt.Fprintln(errs, "clawdline:", err)
		return 1
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(list)
		return 0
	}
	if len(list.Devices) == 0 {
		fmt.Fprintln(out, "nothing is signed in to this machine but this machine itself")
	}
	for _, d := range list.Devices {
		seen := "never"
		if d.LastSeen > 0 {
			seen = time.Unix(d.LastSeen, 0).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(out, "%-24s %-10s %-16s last seen %-16s %s\n", d.ID, strings.Join(d.Caps, "+"),
			time.Unix(d.Created, 0).Format("2006-01-02 15:04"), seen, d.Name)
	}
	if list.Password {
		fmt.Fprintln(out, "a password is set: whoever knows it can sign in another device")
	}
	fmt.Fprintln(out, "this command uses this machine's own key, which is not listed and is not revoked here")
	return 0
}

// devicesRevoke takes one device's key away. It names the device and asks
// first, because a list of ids does not say which one is the browser the
// person is reading this in.
func devicesRevoke(out, errs io.Writer, in io.Reader, door localDoor, args []string) int {
	fs := flag.NewFlagSet("devices revoke", flag.ContinueOnError)
	fs.SetOutput(errs)
	yes := fs.Bool("yes", false, "do not ask")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 || strings.TrimSpace(fs.Arg(0)) == "" {
		devicesUsage(errs)
		return 2
	}
	id := strings.TrimSpace(fs.Arg(0))

	var list contract.DeviceList
	if err := door.do(http.MethodGet, "/v1/auth/devices", &list); err != nil {
		fmt.Fprintln(errs, "clawdline:", err)
		return 1
	}
	var found *contract.PairedDevice
	for i := range list.Devices {
		if list.Devices[i].ID == id {
			found = &list.Devices[i]
		}
	}
	if found == nil {
		fmt.Fprintf(errs, "clawdline: no device signed in to this machine has the id %s (see `clawdline devices`)\n", id)
		return 1
	}
	if !*yes {
		fmt.Fprintf(out, "Revoke %s (%s)? Whatever is signed in with it — this browser too, if it is the one — "+
			"is refused from its next request. [y/N] ", found.ID, found.Name)
		line, _ := bufio.NewReader(in).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "nothing was revoked")
			return 1
		}
	}
	var ok contract.AuthOK
	if err := door.do(http.MethodPost, "/v1/auth/devices/"+url.PathEscape(id)+"/revoke", &ok); err != nil {
		fmt.Fprintln(errs, "clawdline:", err)
		return 1
	}
	fmt.Fprintf(out, "revoked    %s (%s)\n", found.ID, found.Name)
	return 0
}
