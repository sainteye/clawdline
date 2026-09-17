package main

// `clawdline cloud …` — the line to app.clawdline.com, from the command line.
//
// Five subcommands, and the first thing every one of them does is read the
// switch. The line is off until somebody turns it on; `cloud on` is that
// somebody saying so out loud, in a file they can read.
//
//	clawdline cloud status              what the switch and the identity say
//	clawdline cloud on | off            the switch
//	clawdline cloud commands on | off   whether a viewer may act on this Mac
//	clawdline cloud login               register this machine, and wait for approval
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
)

func cloudCommand(args []string) {
	if len(args) == 0 {
		cloudUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "status":
		cloudStatusCommand()
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
	default:
		cloudUsage()
		os.Exit(2)
	}
}

func cloudUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline cloud <status|on|off|commands|login|connect>")
	fmt.Fprintln(os.Stderr, "  status                 the switch, the identity and the endpoints")
	fmt.Fprintln(os.Stderr, "  on | off               turn the cloud line on or off in the settings file")
	fmt.Fprintln(os.Stderr, "  commands on | off      whether a paired viewer may act on this Mac; off by default")
	fmt.Fprintln(os.Stderr, "  login [--wait 10m]     register this machine and wait for the approval")
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

	client := cloud.NewAccountClient(parts.settings.APIBase)
	ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
	defer cancel()

	start, err := client.StartLogin(ctx, machineName, runtime.GOOS, key.PublicKey(), version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	fmt.Printf("code       %s\n", start.UserCode)
	fmt.Printf("approve at %s\n", start.VerificationURIComplete)
	fmt.Printf("waiting    up to %s\n", wait.String())

	interval := time.Duration(start.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(*wait)
	for {
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "clawdline: nobody approved this machine in time")
			os.Exit(1)
		}
		time.Sleep(interval)
		poll, err := client.PollLogin(ctx, start.DeviceCode)
		if err != nil {
			fmt.Fprintln(os.Stderr, "clawdline:", err)
			os.Exit(1)
		}
		switch poll.Status {
		case cloud.LoginPending:
			continue
		case cloud.LoginSlowDown:
			// The server asking for a slower poll is an instruction, not a
			// suggestion: ignoring it is what turns a poll into a rate limit.
			if poll.RetryAfterSeconds > 0 {
				interval = time.Duration(poll.RetryAfterSeconds) * time.Second
			} else {
				interval += time.Second
			}
			continue
		case cloud.LoginDenied:
			fmt.Fprintln(os.Stderr, "clawdline: the approval was declined")
			os.Exit(1)
		case cloud.LoginExpired:
			fmt.Fprintln(os.Stderr, "clawdline: the code expired before it was approved")
			os.Exit(1)
		case cloud.LoginComplete:
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
			fmt.Printf("next       clawdline cloud on && clawdline cloud connect\n")
			return
		default:
			fmt.Fprintf(os.Stderr, "clawdline: the control plane answered %q\n", poll.Status)
			os.Exit(1)
		}
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
		fmt.Fprintln(os.Stderr, "clawdline:", cloud.ErrNoIdentity, "— run `clawdline cloud login`")
		os.Exit(1)
	}
	if identity.APIBase != "" && identity.APIBase != parts.settings.APIBase {
		fmt.Fprintf(os.Stderr, "clawdline: this machine registered with %s, the settings say %s\n",
			identity.APIBase, parts.settings.APIBase)
		os.Exit(1)
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
	if asJSON {
		encoded, _ := json.MarshalIndent(snapshot, "", "  ")
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("state      %s\n", snapshot.State)
		fmt.Printf("connects   %d (reconnects %d)\n", snapshot.Connects, snapshot.Reconnects)
		fmt.Printf("published  %d, acked %d, refused %d\n", snapshot.Published, snapshot.Acked, snapshot.PublishErrors)
		fmt.Printf("inbound    %d accepted, %v dropped\n", snapshot.InboundTotal, snapshot.InboundDropped)
		if snapshot.LastClose != "" {
			fmt.Printf("last close %s\n", snapshot.LastClose)
		}
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
}
