package main

// `clawdline cloud …` — the line to app.clawdline.com, from the command line.
//
// Nine subcommands, and the first thing every one of them does is read the
// switch. The line is off until somebody turns it on; `cloud on` is that
// somebody saying so out loud, in a file they can read.
//
//	clawdline cloud status              what the switch and the identity say
//	clawdline cloud preflight           is the machine side ready for the person's step
//	clawdline cloud on | off            the switch
//	clawdline cloud commands on | off   whether a viewer may act on this Mac
//	clawdline cloud login               register this machine, and wait for approval
//	clawdline cloud pair [--offer …]    hand a browser the account key (cloudpair.go)
//	clawdline cloud devices             who may speak to this Mac
//	clawdline cloud revoke <device>     throw one browser out
//	clawdline cloud rotate              replace this machine's signing key
//	clawdline cloud connect [--for 30s] hold the line open and print what happens
//
// `serve` now opens the same line by itself when the switch is on
// (`main.go`'s startCloudLine), so `connect` is the diagnostic rather than the
// only way to reach the relay: it prints every state change on the terminal and
// exits, which a daemon cannot.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/cloud"
	"github.com/sainteye/clawdline-go/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline-go/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline-go/internal/config"
	domaincloud "github.com/sainteye/clawdline-go/internal/domain/cloud"
	cloudtransport "github.com/sainteye/clawdline-go/internal/transport/cloud"
)

func cloudCommand(args []string) {
	if len(args) == 0 {
		cloudUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "status":
		cloudStatusCommand()
	case "preflight":
		cloudPreflightCommand()
	case "on":
		cloudSwitchCommand(true)
	case "off":
		cloudSwitchCommand(false)
	case "commands":
		cloudCommandsCommand(args[1:])
	case "login":
		cloudLoginCommand(args[1:])
	case "connect":
		cloudConnectCommand(args[1:])
	case "pair":
		cloudPairCommand(args[1:])
	case "devices":
		cloudDevicesCommand()
	case "revoke":
		cloudRevokeCommand(args[1:])
	case "rotate":
		cloudRotateCommand(args[1:])
	default:
		cloudUsage()
		os.Exit(2)
	}
}

func cloudUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline cloud <status|preflight|on|off|commands|login|pair|devices|revoke|rotate|connect>")
	fmt.Fprintln(os.Stderr, "  status                 the switch, the identity and the endpoints")
	fmt.Fprintln(os.Stderr, "  preflight              check, without the network, that only the person's step is left")
	fmt.Fprintln(os.Stderr, "  on | off               turn the cloud line on or off in the settings file")
	fmt.Fprintln(os.Stderr, "  commands on | off      whether a paired viewer may act on this Mac; off by default")
	fmt.Fprintln(os.Stderr, "  login [--wait 10m]     register this machine and wait for the approval")
	fmt.Fprintln(os.Stderr, "  pair [--offer <code>]  show a browser a one-time link, or finish with its code")
	fmt.Fprintln(os.Stderr, "  devices                who may speak to this Mac, and where that trust came from")
	fmt.Fprintln(os.Stderr, "  revoke <device-id>     throw one browser out of this Mac")
	fmt.Fprintln(os.Stderr, "  rotate [--yes]         replace this machine's signing key; every browser re-pairs")
	fmt.Fprintln(os.Stderr, "  connect [--for 1m]     hold the line open and report what happens")
}

// cloudParts is everything the cloud commands share: the settings file, the
// key store, the identity record and the endpoints.
type cloudParts struct {
	cfg      config.Config
	settings cloud.Settings
	keys     *cloudkeys.Files
	identity *cloud.IdentityStore
	file     *nextconfig.File
}

// foreignDirs names the Swift app's settings directory, so that nothing here
// can read or write it. It is spelled as the fixed path that app always uses,
// not as whatever this process's environment says.
func foreignDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".config", "clawdline")}
}

func openCloud() (cloudParts, error) {
	cfg := config.Load()
	file := nextconfig.Open(cfg.Dir, foreignDirs()...)
	settings, err := cloud.ReadSettings(file)
	if err != nil {
		return cloudParts{}, err
	}
	keys, err := cloudkeys.Open(cfg.Dir, foreignDirs()...)
	if err != nil {
		return cloudParts{}, err
	}
	return cloudParts{
		cfg:      cfg,
		settings: settings,
		keys:     keys,
		identity: cloud.NewIdentityStore(keys.Dir()),
		file:     file,
	}, nil
}

func cloudStatusCommand() {
	parts, err := openCloud()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("enabled    %v\n", parts.settings.Enabled)
	fmt.Printf("commands   %v\n", parts.settings.Commands)
	fmt.Printf("relay      %s\n", parts.settings.RelayURL)
	fmt.Printf("api        %s\n", parts.settings.APIBase)
	fmt.Printf("keys       %s\n", parts.keys.Dir())

	identity, found, err := parts.identity.Load()
	switch {
	case err != nil:
		// An identity that exists and cannot be read is reported as itself.
		// Printing "not registered" here is how a permission error turns into
		// a second machine registration.
		fmt.Printf("identity   unreadable: %v\n", err)
		os.Exit(1)
	case !found:
		fmt.Printf("identity   none — run `clawdline cloud login`\n")
	default:
		fmt.Printf("account    %s\n", identity.AccountID)
		fmt.Printf("machine    %s\n", identity.MachineID)
		fmt.Printf("registered %s\n", identity.APIBase)
		if err := cloud.CheckEnvironment(identity, parts.settings); err != nil {
			fmt.Printf("mismatch   %s\n", cloud.DescribeFailure(err))
		}
	}

	key, found, err := parts.keys.DeviceKey()
	switch {
	case err != nil:
		fmt.Printf("device key unreadable: %v\n", err)
		os.Exit(1)
	case !found:
		fmt.Printf("device key none yet\n")
	default:
		fmt.Printf("device key %s\n", key.Fingerprint())
	}
}

func cloudSwitchCommand(on bool) {
	parts, err := openCloud()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if err := cloud.SetEnabled(parts.file, on); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if on {
		fmt.Printf("cloud on   %s\n", parts.file.Path())
		return
	}
	fmt.Printf("cloud off  %s\n", parts.file.Path())
}

// cloudCommandsCommand is the remote-write switch, which is a separate
// decision from whether the line is up: reading a session list and running code
// on this Mac are not the same permission and never share a switch.
func cloudCommandsCommand(args []string) {
	if len(args) != 1 || (args[0] != "on" && args[0] != "off") {
		fmt.Fprintln(os.Stderr, "usage: clawdline cloud commands <on|off>")
		os.Exit(2)
	}
	parts, err := openCloud()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	on := args[0] == "on"
	if err := cloud.SetCommands(parts.file, on); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("commands %-3s %s\n", args[0], parts.file.Path())
	if on {
		fmt.Printf("           a paired viewer may now type into this Mac's sessions\n")
	}
}

func cloudLoginCommand(args []string) {
	fs := flag.NewFlagSet("cloud login", flag.ExitOnError)
	wait := fs.Duration("wait", 10*time.Minute, "how long to wait for the approval")
	name := fs.String("name", "", "what to call this machine; defaults to the hostname")
	_ = fs.Parse(args)

	parts, err := openCloud()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	// The key is minted here if this machine has never had one. It is **not**
	// the Swift app's key: that app is on this Mac too, and one sender id with
	// two producers means two sequence counters over one replay window.
	key, err := domaincloud.LoadOrCreateDeviceKey(parts.keys, rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	machineName := *name
	if machineName == "" {
		machineName = parts.settings.MachineName
	}
	if machineName == "" {
		host, _ := os.Hostname()
		machineName = strings.TrimSuffix(host, ".local")
	}
	if machineName == "" {
		machineName = "clawdline-next"
	}

	// Signing in again over an identity is allowed — it is how a machine moves
	// from a local test control plane to production — but it is said out loud.
	if previous, found, err := parts.identity.Load(); err != nil {
		cloudFail(err)
	} else if found {
		fmt.Printf("replacing  machine %s registered with %s\n", previous.MachineID, previous.APIBase)
	}

	client := cloud.NewAccountClient(parts.settings.APIBase)
	ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
	defer cancel()

	fmt.Printf("api        %s\n", parts.settings.APIBase)
	start, err := client.StartLogin(ctx, machineName, runtime.GOOS, key.PublicKey(), version)
	if err != nil {
		cloudFail(err)
	}
	fmt.Printf("code       %s\n", start.UserCode)
	fmt.Printf("approve at %s\n", start.VerificationURIComplete)
	fmt.Printf("waiting    up to %s\n", wait.String())

	poll, err := client.WaitForApproval(ctx, start, *wait, nil)
	if err != nil {
		cloudFail(err)
	}
	identity := cloud.Identity{
		AccountID:         poll.AccountID,
		MachineID:         poll.MachineID,
		MachineCredential: poll.MachineCredential,
		APIBase:           parts.settings.APIBase,
		Name:              machineName,
	}
	if err := parts.identity.Save(identity); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("account    %s\n", identity.AccountID)
	fmt.Printf("machine    %s\n", identity.MachineID)
	fmt.Printf("saved      %s\n", parts.identity.Path())
	// The approval is proven by using it once: the credential buys a device
	// token or it does not. Nothing is connected — the relay is not dialled.
	token, err := client.MintDeviceToken(ctx, identity.MachineCredential)
	if err != nil {
		cloudFail(err)
	}
	fmt.Printf("verified   the control plane issued a device token (expires %s)\n", token.ExpiresAt.Local().Format(time.RFC3339))
	fmt.Printf("next       clawdline cloud on, then restart the daemon\n")
}

// cloudFail prints a failure by its name and what to do about it, and exits.
func cloudFail(err error) {
	failure := cloud.DescribeFailure(err)
	if failure.Kind == "" {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "clawdline: %s\n", failure)
	fmt.Fprintf(os.Stderr, "what to do %s\n", failure.Remedy())
	os.Exit(1)
}

// cloudPreflightCommand says whether the machine side of the production
// cutover is done, touching nothing on the network.
func cloudPreflightCommand() {
	cfg := config.Load()
	report := cloudtransport.Preflight(cloudtransport.LinkOptions{Dir: cfg.Dir, ForeignDirs: foreignDirs()})
	for _, check := range report.Checks {
		fmt.Printf("%-5s %-10s %s\n", check.Result, check.Name, check.Detail)
	}
	fmt.Printf("next       %s\n", report.Next)
	fmt.Printf("network    nothing in this check was sent anywhere\n")
	if !report.Ready {
		os.Exit(1)
	}
}

func cloudConnectCommand(args []string) {
	fs := flag.NewFlagSet("cloud connect", flag.ExitOnError)
	hold := fs.Duration("for", 0, "how long to hold the line; 0 means until interrupted")
	publish := fs.String("publish", "", "a channel to publish one test envelope on")
	body := fs.String("body", `{"type":"probe","v":1}`, "the plaintext of that envelope")
	repeat := fs.Int("repeat-sequence", 0, "re-send the published envelope's exact bytes N more times, to show the relay's answer to a duplicate")
	statusJSON := fs.Bool("json", false, "print the final status as JSON")
	_ = fs.Parse(args)

	parts, err := openCloud()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if !parts.settings.Enabled {
		// The switch is the whole answer. Connecting "just this once" because
		// a command was typed would make the setting a decoration.
		fmt.Fprintln(os.Stderr, "clawdline:", cloud.ErrDisabled, "— run `clawdline cloud on`")
		os.Exit(1)
	}
	identity, found, err := parts.identity.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if !found {
		cloudFail(cloud.ErrNoIdentity)
	}
	if err := cloud.CheckEnvironment(identity, parts.settings); err != nil {
		cloudFail(err)
	}

	key, err := domaincloud.LoadOrCreateDeviceKey(parts.keys, rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	secret, err := domaincloud.LoadOrCreateMasterSecret(parts.keys, rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	fence := cloud.NewFileFence(parts.keys.Dir(), identity.MachineID)
	spool, err := cloud.NewSpool(cloud.DefaultSpoolLimits(), fence, time.Now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	status := cloud.NewStatusRecorder(time.Now())
	status.SetEnabled(true)
	status.SetIdentity(identity.AccountID, identity.MachineID, parts.settings.RelayURL)

	transport, err := cloud.New(cloud.Options{
		RelayURL: parts.settings.RelayURL,
		Role:     "machine",
		Token:    &cloud.TokenSource{Client: cloud.NewAccountClient(parts.settings.APIBase), Credential: identity.MachineCredential},
		Identity: identity,
		Signer:   key,
		Spool:    spool,
		Status:   status,
		Replay:   domaincloud.NewReplayWindow(0),
		// Every viewer's key would come from the paired-device roster, which
		// is the next wave's work. Until then nothing inbound authenticates,
		// and the status file says so as `unknown_sender` rather than by
		// accepting anything.
		PublicKeyFor: func(string) (ed25519.PublicKey, bool) { return nil, false },
		ContentKey:   secret,
		Log:          func(line string) { fmt.Println(line) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *hold > 0 {
		ctx, cancel = context.WithTimeout(ctx, *hold)
		defer cancel()
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-interrupted
		cancel()
	}()

	done := make(chan error, 1)
	go func() { done <- transport.Run(ctx) }()

	select {
	case <-transport.Ready():
	case err := <-done:
		reportCloudExit(err, status, *statusJSON)
		return
	case <-ctx.Done():
		reportCloudExit(ctx.Err(), status, *statusJSON)
		return
	}

	if *publish != "" {
		if err := publishProbe(spool, transport, *publish, identity.MachineID, key, secret, []byte(*body), *repeat); err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
		}
	}

	<-ctx.Done()
	transport.Stop()
	<-done
	reportCloudExit(nil, status, *statusJSON)
}

// publishProbe seals one envelope, sends it, and optionally sends the *same
// bytes* again.
//
// Re-sending the identical bytes is not a mistake, it is the point: it is how
// a duplicate sequence is shown to be handled, and doing it with a fresh seal
// would be a different envelope with a different sequence and would prove
// nothing.
func publishProbe(spool *cloud.Spool, transport *cloud.Transport, channel, machine string, key domaincloud.DeviceKey, secret domaincloud.ContentKey, body []byte, repeat int) error {
	class, ok := domaincloud.ChannelClasses(channel)
	if !ok || len(class) == 0 {
		return fmt.Errorf("%q is not a channel this build knows", channel)
	}
	if err := domaincloud.ProducibleChannel(channel); err != nil {
		return err
	}
	kind := cloud.SpoolChannel(strings.SplitN(channel, "/", 2)[0])

	var seq uint64
	var err error
	if kind.IsLatestValue() {
		seq, err = spool.ReserveLatestValue(kind, channel, channel, len(body))
	} else {
		seq, err = spool.Reserve(kind, channel, channel, len(body))
	}
	if err != nil {
		return err
	}

	now := time.Now()
	envelope, err := domaincloud.Seal(body, domaincloud.SealParams{
		Ch:     channel,
		Seq:    seq,
		Ts:     uint64(now.UnixMilli()),
		Class:  class[0],
		KeyID:  cloud.MasterKeyID,
		Sender: machine,
		Key:    secret,
		Signer: key,
		Rand:   rand.Reader,
	})
	if err != nil {
		return err
	}
	sealed, err := envelope.CanonicalJSON()
	if err != nil {
		return err
	}
	if err := spool.Seal(seq, sealed, now); err != nil {
		return err
	}
	fmt.Printf("cloud sealed seq=%d ch=%s bytes=%d\n", seq, channel, len(sealed))

	// The drain worker picks the row up; this only waits for it to leave.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if row, ok := spool.Row(seq); ok && row.State != "reserved" && row.State != "ready" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	for i := 0; i < repeat; i++ {
		time.Sleep(300 * time.Millisecond)
		if err := transport.Publish(sealed); err != nil {
			return err
		}
		fmt.Printf("cloud re-published the identical bytes seq=%d (attempt %d)\n", seq, i+2)
	}
	return nil
}

func reportCloudExit(err error, status *cloud.StatusRecorder, asJSON bool) {
	snapshot := status.Snapshot()
	stopped := err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	if asJSON {
		encoded, _ := json.MarshalIndent(snapshot, "", "  ")
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("state      %s\n", snapshot.State)
		fmt.Printf("connects   %d (reconnects %d)\n", snapshot.Connects, snapshot.Reconnects)
		fmt.Printf("published  %d, acked %d, refused %d\n", snapshot.Published, snapshot.Acked, snapshot.PublishErrors)
		fmt.Printf("inbound    %d accepted, %v dropped\n", snapshot.InboundTotal, snapshot.InboundDropped)
		if last := (cloud.LineFailure{Kind: cloud.KindOfCode(snapshot.LastClose), Code: snapshot.LastClose}); last.Kind != "" {
			fmt.Printf("last close %s\n", last)
			if !stopped {
				// A line that stopped says what to do once, below.
				fmt.Printf("what to do %s\n", last.Remedy())
			}
		}
	}
	if stopped {
		cloudFail(err)
	}
}
