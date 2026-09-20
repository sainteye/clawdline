package cloud

// The production cutover's last check before a person is asked to do anything.
//
// docs/cloud-cutover.md stops every step that touches the person's account at
// the moment before it, and this is what says the machine side is ready for
// that moment: the settings name the production endpoints and nothing else,
// this app's key directory is its own, and whatever identity is on disk is
// accounted for. **It sends nothing anywhere.** It reads the settings file and
// this app's own cloud directory, and the only thing it can write is the empty
// 0700 key directory that `clawdline cloud status` also creates.
//
// Each row is ok, warn or block. A block means the next step would fail or do
// the wrong thing — a leftover local relay URL would register this machine with a
// test control plane, for instance — and it is refused here, where it costs
// nothing, rather than discovered after the person has approved a code.

import (
	"errors"
	"fmt"

	adaptercloud "github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
)

// Preflight results.
const (
	PreflightOK    = "ok"
	PreflightWarn  = "warn"
	PreflightBlock = "block"
)

// PreflightCheck is one row.
type PreflightCheck struct {
	Name   string `json:"name"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// PreflightReport is the whole answer.
type PreflightReport struct {
	Checks []PreflightCheck `json:"checks"`
	// Ready is true when no row blocks.
	Ready bool `json:"ready"`
	// Next is the one step that comes after this, and whose it is.
	Next string `json:"next"`
}

// Preflight reads what the cutover depends on. Dir and ForeignDirs are the
// only fields of opts it uses.
func Preflight(opts LinkOptions) PreflightReport {
	var report PreflightReport
	add := func(name, result, format string, args ...any) {
		report.Checks = append(report.Checks, PreflightCheck{Name: name, Result: result, Detail: fmt.Sprintf(format, args...)})
	}

	settings, err := adaptercloud.ReadSettings(nextconfig.Open(opts.Dir, opts.ForeignDirs...))
	if err != nil {
		add("settings", PreflightBlock, "the settings file cannot be used: %v", err)
		report.Next = "fix the settings file, then run this again"
		return report
	}
	add("settings", PreflightOK, "readable")

	production := settings.APIBase == adaptercloud.DefaultAPIBase &&
		settings.RelayURL == adaptercloud.DefaultRelayURL &&
		settings.AppOrigin == adaptercloud.DefaultAppOrigin
	switch {
	case production:
		add("endpoints", PreflightOK, "production: %s, %s, %s", settings.APIBase, settings.RelayURL, settings.AppOrigin)
	case settings.APIBase == adaptercloud.DefaultAPIBase || settings.RelayURL == adaptercloud.DefaultRelayURL:
		// Half production and half something else is the setting that turns
		// into `relay_unauthorized` for ever: a token minted by one control
		// plane, presented to the other's relay.
		add("endpoints", PreflightBlock, "mixed environments: api %s, relay %s, console %s — remove %s, %s and %s so all three are production",
			settings.APIBase, settings.RelayURL, settings.AppOrigin, adaptercloud.KeyAPIBase, adaptercloud.KeyRelayURL, adaptercloud.KeyAppOrigin)
	default:
		add("endpoints", PreflightBlock, "not production: api %s, relay %s — a local test setting is still in the file; remove %s, %s and %s",
			settings.APIBase, settings.RelayURL, adaptercloud.KeyAPIBase, adaptercloud.KeyRelayURL, adaptercloud.KeyAppOrigin)
	}

	if settings.Enabled {
		add("switch", PreflightWarn, "%s is already true: the daemon connects as soon as it restarts with an identity", adaptercloud.KeyEnabled)
	} else {
		add("switch", PreflightOK, "%s is off; it is turned on after sign-in (step 3)", adaptercloud.KeyEnabled)
	}
	if settings.Commands {
		add("commands", PreflightWarn, "%s is already true: a browser paired with this machine could type into its sessions; the runbook turns this on last", adaptercloud.KeyCommands)
	} else {
		add("commands", PreflightOK, "%s is off: a paired browser may read and may not act", adaptercloud.KeyCommands)
	}

	keys, err := cloudkeys.Open(opts.Dir, opts.ForeignDirs...)
	if err != nil {
		if errors.Is(err, cloudkeys.ErrForeignDir) {
			add("keys", PreflightBlock, "this state directory is the Swift app's: %v", err)
		} else {
			add("keys", PreflightBlock, "the key directory cannot be opened: %v", err)
		}
		report.Next = "fix the key directory, then run this again"
		return report
	}
	add("keys", PreflightOK, "this app's own directory, %s (the Swift app's ~/.config/clawdline is refused by construction)", keys.Dir())

	identity, found, err := adaptercloud.NewIdentityStore(keys.Dir()).Load()
	signedIn := false
	switch {
	case err != nil:
		// An identity that exists and cannot be read is never "none": signing
		// in over it would register a second machine for the same host.
		add("identity", PreflightBlock, "exists and cannot be read: %v", err)
	case !found:
		add("identity", PreflightOK, "none yet; `clawdline cloud login` creates it")
	case adaptercloud.CheckEnvironment(identity, settings) != nil:
		add("identity", PreflightWarn, "machine %s is registered with %s, not %s; signing in replaces it (the old one stays in that control plane's list)",
			identity.MachineID, identity.APIBase, settings.APIBase)
	default:
		signedIn = true
		add("identity", PreflightOK, "signed in: machine %s of account %s", identity.MachineID, identity.AccountID)
	}

	key, found, err := keys.DeviceKey()
	switch {
	case err != nil:
		add("device key", PreflightBlock, "exists and cannot be read: %v", err)
	case !found:
		add("device key", PreflightOK, "none yet; sign-in creates it under this app's directory")
	default:
		add("device key", PreflightOK, "%s", key.Fingerprint())
	}

	report.Ready = true
	for _, check := range report.Checks {
		if check.Result == PreflightBlock {
			report.Ready = false
		}
	}
	switch {
	case !report.Ready:
		report.Next = "clear every block above, then run this again"
	case !signedIn:
		report.Next = "the person's step: `clawdline cloud login`, then approve the code in the browser (docs/cloud-cutover.md step 2)"
	case !settings.Enabled:
		report.Next = "`clawdline cloud on`, then restart the daemon (docs/cloud-cutover.md step 3)"
	default:
		report.Next = "restart the daemon if it has not been since sign-in, then read `clawdline cloud status` (docs/cloud-cutover.md step 3)"
	}
	return report
}
