package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// cloud viewer is a separate principal from cloud login's machine role.
func cloudViewerCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "usage", "usage: clawdline cloud viewer <login|status|machines|pair>"))
		os.Exit(2)
	}
	switch args[0] {
	case "pair":
		cloudViewerPairCommand(args[1:])
	case "machines":
		cloudViewerMachinesCommand(args[1:])
	case "login":
		fs := flag.NewFlagSet("cloud viewer login", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		wait := fs.Duration("wait", 10*time.Minute, cliCopy("viewer", "wait_help", "how long to wait for approval"))
		name := fs.String("name", "Clawdline CLI ("+runtime.GOOS+")", cliCopy("viewer", "name_help", "name of this viewer device"))
		if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *wait <= 0 || *name == "" {
			fmt.Fprintln(os.Stderr, cliCopy("viewer", "login_usage", "usage: clawdline cloud viewer login [--wait 10m] [--name label]"))
			os.Exit(2)
		}
		parts, err := openCloud()
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
		defer cancel()
		store := cloud.NewViewerIdentityStore(parts.keys.Dir())
		code := viewerLoginRun(ctx, os.Stdout, os.Stderr, cloud.NewAccountClient(parts.settings.APIBase), parts.keys, store, parts.settings.APIBase, *name, *wait, nil)
		os.Exit(code)
	case "status":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, cliCopy("viewer", "usage", "usage: clawdline cloud viewer <login|status|machines|pair>"))
			os.Exit(2)
		}
		parts, err := openCloud()
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
			os.Exit(1)
		}
		identity, found, err := cloud.NewViewerIdentityStore(parts.keys.Dir()).Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "clawdline cloud viewer: identity_unreadable: %v\n", err)
			os.Exit(1)
		}
		if !found {
			fmt.Fprintln(os.Stdout, cliCopy("viewer", "status_none", "viewer authorization is absent; run `clawdline cloud viewer login`"))
			return
		}
		key, keyFound, err := parts.keys.ViewerKey()
		if err != nil || !keyFound {
			fmt.Fprintln(os.Stderr, cliCopy("viewer", "key_missing", "viewer_key_missing: the viewer signing key is missing or unreadable"))
			os.Exit(1)
		}
		if identity.APIBase != parts.settings.APIBase {
			fmt.Fprintln(os.Stderr, cliCopy("viewer", "environment_mismatch", "viewer_environment_mismatch: this identity belongs to another Cloud API"))
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, cliCopy("viewer", "status_ready", "viewer device %s on account %s; key %s; use `viewer machines` to inspect pairing\n"), identity.DeviceID, identity.AccountID, key.Fingerprint())
	default:
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "usage", "usage: clawdline cloud viewer <login|status|machines|pair>"))
		os.Exit(2)
	}
}

func viewerLoginRun(ctx context.Context, stdout, stderr io.Writer, client *cloud.AccountClient, keys *cloudkeys.Files, store *cloud.ViewerIdentityStore, apiBase, name string, wait time.Duration, sleep func(context.Context, time.Duration) error) int {
	key, found, err := keys.ViewerKey()
	if err != nil {
		fmt.Fprintf(stderr, "clawdline cloud viewer: viewer_key_unreadable: %v\n", err)
		return 1
	}
	if !found {
		key, err = domaincloud.NewDeviceKey(nil)
		if err != nil {
			fmt.Fprintf(stderr, "clawdline cloud viewer: key_generation_failed: %v\n", err)
			return 1
		}
	}
	start, err := client.StartViewerLogin(ctx, name, key.PublicKey())
	if err != nil {
		var api *cloud.APIError
		if errors.As(err, &api) && (api.Code == "bad_machine" || api.Code == "unsupported_device_kind" || api.Status == 404) {
			fmt.Fprintln(stderr, cliCopy("viewer", "flow_unavailable", "viewer_flow_unavailable: this Cloud API cannot authorize a CLI viewer device yet"))
			return 1
		}
		fmt.Fprintf(stderr, "clawdline cloud viewer: authorization_start_failed: %v\n", err)
		return 1
	}
	if !found {
		if err := keys.SaveViewerKey(key); err != nil {
			fmt.Fprintf(stderr, "clawdline cloud viewer: key_save_failed: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(stdout, cliCopy("viewer", "approve", "code %s\napprove at %s\n"), start.UserCode, start.VerificationURIComplete)
	poll, err := client.WaitForViewerApproval(ctx, start, wait, sleep)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline cloud viewer: authorization_%s: %v\n", viewerFailureCode(err), err)
		return 1
	}
	identity := cloud.ViewerIdentity{AccountID: poll.AccountID, DeviceID: poll.DeviceID, ViewerCredential: poll.ViewerCredential, APIBase: apiBase}
	// The token proves that the credential is accepted as a viewer by this API.
	// No token or credential is ever printed. Pairing is a separate grant.
	token, err := client.MintDeviceToken(ctx, identity.ViewerCredential)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline cloud viewer: token_refused: %v\n", err)
		return 1
	}
	if !cloud.ViewerTokenMatches(token.Token, identity.AccountID, identity.DeviceID, key.PublicKey()) {
		fmt.Fprintln(stderr, cliCopy("viewer", "wrong_token_role", "viewer_token_mismatch: the Cloud API did not mint a token for this viewer key"))
		return 1
	}
	if err := store.Save(identity); err != nil {
		fmt.Fprintf(stderr, "clawdline cloud viewer: identity_save_failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, cliCopy("viewer", "authorized", "authorized viewer device %s; pair each target machine before reading it\n"), identity.DeviceID)
	return 0
}

func viewerFailureCode(err error) string {
	switch {
	case errors.Is(err, cloud.ErrLoginDenied):
		return "denied"
	case errors.Is(err, cloud.ErrLoginExpired):
		return "expired"
	case errors.Is(err, cloud.ErrLoginTimeout):
		return "timeout"
	default:
		return "failed"
	}
}
