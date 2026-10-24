package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
	"github.com/sainteye/clawdline/internal/adapters/healthsealed"
	"github.com/sainteye/clawdline/internal/adapters/healthupload"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/connectionhealth"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// connectionHealthOffEnv turns the connection-health runtime off for a boot.
// Off is a supported state: the call sites receive a nil tracker and record
// nothing, and the local route answers `unsupported`.
const connectionHealthOffEnv = "CLAWDLINE_CONNECTION_HEALTH"

// connectionHealthStoreDir is the durable store's directory under the
// daemon's state directory.
func connectionHealthStoreDir(stateDir string) string {
	return filepath.Join(stateDir, "connection-health", "journal")
}

// startConnectionHealth makes the one runtime of this boot, hands it to the
// server, runs its watchdog for the daemon's lifetime, opens the durable
// store beside it and starts the health uploader. A runtime that cannot be
// made (no random bytes for a boot id) leaves monitoring off and says so; it
// never stops the daemon. The answer is nil when monitoring is off.
//
// The store is opened on its own goroutine: Open scans every segment (0.57 s
// on a full 96 MiB store, docs/admin-connection-health-storage.md §8) and a
// broken disk may make it slow or fail, and neither may hold the port or
// the first request. Until it opens the runtime is memory only and the
// route says `opening`; a failed open is reported with its code and the
// runtime stays memory only. Once open, the watchdog's one journal writer
// offers each record it appended, as stamped, to the store's non-blocking
// Offer (Watchdog.Attach).
//
// The uploader (internal/adapters/healthupload) runs on its own goroutine
// with its own HTTPS client. It makes no request unless the person opted in
// (`cloud_health_upload: true`), Cloud is on and this machine has an
// identity, and it reads only the durable store, never the Cloud line.
//
// The owner-sealed diagnostics worker (internal/adapters/healthsealed) runs
// beside it on its own client: off unless the person opted in to owner
// retention and Cloud is on, and refused before any request until the
// identity pairing has recorded the master secret's content-key epoch
// (docs/health-identity-handover.md).
func startConnectionHealth(stateDir string, srv *httptransport.Server) *connectionHealthLife {
	if strings.EqualFold(os.Getenv(connectionHealthOffEnv), "off") {
		log.Printf("connection health: off (%s=off)", connectionHealthOffEnv)
		return nil
	}
	build := buildRevision()
	if build == "" {
		build = version
	}
	rt, err := connectionhealth.NewRuntime(build, connectionhealth.DefaultCadence)
	if err != nil {
		log.Printf("connection health: off, the runtime could not be made: %v", err)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &connectionHealthLife{rt: rt, cancel: cancel,
		runDone: make(chan struct{}), opened: make(chan struct{}), uploadDone: make(chan struct{}),
		sealedDone: make(chan struct{})}
	sealed, sealedErr := newSealedDiagnostics(stateDir, foreignDirs(), sealedMachines)
	if sealedErr != nil {
		log.Printf("connection health: owner-sealed diagnostics are unavailable (%s)", healthsealed.CodeOf(sealedErr))
	}
	h.sealed = sealed
	rt.Capture.SetEnabled(stallCaptureOn())
	feed := sealedFeed(rt, sealed)
	srv.SetConnectionHealth(rt)
	srv.SetConnectionHealthStore(nil, nil, true)
	go func() {
		defer close(h.runDone)
		rt.Run(ctx)
	}()
	go func() {
		defer close(h.opened)
		store, err := openConnectionHealthStore(rt, stateDir, feed)
		if err == nil && ctx.Err() != nil {
			// The daemon began stopping while the store was opening: close
			// it here, or its lock and handles outlive the shutdown.
			closeCtx, done := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
			_ = store.Close(closeCtx)
			done()
			return
		}
		srv.SetConnectionHealthStore(store, err, false)
		if err != nil {
			// Owner diagnostics read the journal's own records, not the
			// store, so they are fed even when the store cannot open.
			if feed != nil {
				rt.Watchdog.Attach(feed)
			}
			log.Printf("connection health: the durable store could not be opened (%s); this boot is memory only", healthjournal.CodeOf(err))
			return
		}
		h.store.Store(store)
	}()
	h.upload = healthupload.New(healthupload.Options{
		StateDir: connectionHealthUploadDir(stateDir),
		Identity: healthUploadIdentity(stateDir, foreignDirs()),
		Source: func() healthupload.Source {
			if s := h.store.Load(); s != nil {
				return s
			}
			return nil
		},
		Log:              func(format string, args ...any) { log.Printf(format, args...) },
		BootID:           rt.BootID,
		OwnerDiagnostics: ownerDiagnosticsStatus(sealed, sealedErr),
	})
	srv.SetConnectionHealthUpload(h.upload.Status)
	go func() {
		defer close(h.uploadDone)
		h.upload.Run(ctx)
	}()
	go func() {
		defer close(h.sealedDone)
		if sealed != nil {
			sealed.Run(ctx)
		}
	}()
	return h
}

// connectionHealthStopTimeout bounds the whole connection-health shutdown:
// a stuck disk may cost this much of a stopping daemon, never more.
const connectionHealthStopTimeout = 3 * time.Second

// connectionHealthLife is the running monitor: its watchdog, its store once
// open, and its uploader.
type connectionHealthLife struct {
	rt         *connectionhealth.Runtime
	upload     *healthupload.Uploader
	sealed     *healthsealed.Worker
	cancel     context.CancelFunc
	store      atomic.Pointer[healthjournal.Store]
	runDone    chan struct{}
	opened     chan struct{}
	uploadDone chan struct{}
	sealedDone chan struct{}
}

// Stop ends the producers first (the watchdog's scans, then its journal
// writer, which drains every transition already observed into the journal
// and the store's queue, the uploader and the sealed diagnostics worker,
// whose revision in flight stays pending on disk with its exact body for
// the next boot), then flushes and closes the
// store, all within ctx. A part that does not finish in time is named in
// the error and left behind — a held watchdog writer with how many observed
// transitions it had not yet journaled — and the store is still flushed and
// closed; Stop never waits past ctx and starts no goroutine of its own.
func (h *connectionHealthLife) Stop(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.cancel()
	var late []string
	for _, w := range []struct {
		name string
		done <-chan struct{}
	}{{"watchdog", h.runDone}, {"uploader", h.uploadDone}, {"sealed diagnostics", h.sealedDone}, {"store open", h.opened}} {
		select {
		case <-w.done:
		case <-ctx.Done():
			if w.done == h.runDone {
				queued, _ := h.rt.Watchdog.Pending()
				late = append(late, fmt.Sprintf("watchdog (writer held; %d observed transitions not journaled)", queued))
				continue
			}
			late = append(late, w.name)
		}
	}
	if s := h.store.Load(); s != nil {
		if _, err := s.Flush(ctx); err != nil {
			late = append(late, "store flush ("+string(healthjournal.CodeOf(err))+")")
		}
		if err := s.Close(ctx); err != nil {
			late = append(late, "store close ("+string(healthjournal.CodeOf(err))+")")
		}
	}
	if len(late) > 0 {
		return fmt.Errorf("connection health did not stop cleanly: %s", strings.Join(late, ", "))
	}
	return nil
}

// stopConnectionHealthOnSignal stops the monitor when the daemon is told to
// stop, then lets the signal do what it did before: the process ends the
// way the service manager expects.
func stopConnectionHealthOnSignal(h *connectionHealthLife, stopCloud context.CancelFunc) {
	if h == nil && stopCloud == nil {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	sig := <-signals
	// Close the deferred activation gate before flushing health. In particular,
	// an update accepted during that flush must not open a new Cloud link.
	if stopCloud != nil {
		stopCloud()
	}
	if h != nil {
		ctx, cancel := context.WithTimeout(context.Background(), connectionHealthStopTimeout)
		if err := h.Stop(ctx); err != nil {
			log.Printf("%v", err)
		} else {
			log.Printf("connection health: stopped; the durable store is flushed and closed")
		}
		cancel()
	}
	signal.Reset(os.Interrupt, syscall.SIGTERM)
	if p, err := os.FindProcess(os.Getpid()); err == nil && p.Signal(sig) == nil {
		// The default action ends the process; this wait is never reached
		// unless the signal is ignored by the platform.
		time.Sleep(time.Second)
	}
	os.Exit(1)
}

// healthUploadKey is the opt-in switch in the app's settings file. Anything
// but a JSON true is off, and off makes no request.
const healthUploadKey = "cloud_health_upload"

// connectionHealthUploadDir holds the uploader's progress file.
func connectionHealthUploadDir(stateDir string) string {
	return filepath.Join(stateDir, "connection-health", "upload")
}

// healthUploadIdentity reads, on every uploader cycle and from local files
// only, whether uploading is allowed and with which identity: the opt-in,
// Cloud's own switch, and the machine identity of the control plane the
// settings name. A mismatch between the two is refused, never resolved by
// picking one.
func healthUploadIdentity(stateDir string, foreign []string) healthupload.IdentityProvider {
	return func() (healthupload.Identity, error) {
		file := nextconfig.Open(stateDir, foreign...)
		values, err := file.Read()
		if err != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		}
		var on bool
		if raw, ok := values.Raw[healthUploadKey]; !ok || json.Unmarshal(raw, &on) != nil || !on {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeNotOptedIn}
		}
		settings, err := adaptercloud.ReadSettings(file)
		if err != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		}
		if !settings.Enabled {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeCloudOff}
		}
		identity, found, err := adaptercloud.NewIdentityStore(filepath.Join(stateDir, cloudkeys.DirName)).Load()
		switch {
		case err != nil:
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeIdentityUnread}
		case !found || !identity.Valid():
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeNoIdentity}
		}
		if adaptercloud.CheckEnvironment(identity, settings) != nil {
			return healthupload.Identity{}, &healthupload.Error{Code: healthupload.CodeOtherOrigin}
		}
		return healthupload.Identity{APIBase: settings.APIBase, Credential: identity.MachineCredential}, nil
	}
}

// openConnectionHealthStore opens the durable store for rt's boot and
// attaches it, and also when set, to rt's journal writer. also must not
// block: it runs on the writer's goroutine.
func openConnectionHealthStore(rt *connectionhealth.Runtime, stateDir string, also func(connectionhealth.Record)) (*healthjournal.Store, error) {
	dir, err := filepath.Abs(connectionHealthStoreDir(stateDir))
	if err != nil {
		return nil, err
	}
	store, err := healthjournal.Open(healthjournal.Options{Dir: filepath.Clean(dir), BootID: rt.BootID, Build: rt.Build})
	if err != nil {
		return nil, err
	}
	rt.Watchdog.Attach(func(r connectionhealth.Record) {
		_, _ = store.Offer(r)
		if also != nil {
			also(r)
		}
	})
	return store, nil
}

// connectionHealthSealedDir holds the owner-sealed diagnostics' local
// attachment records (internal/adapters/healthsealed.Store).
func connectionHealthSealedDir(stateDir string) string {
	return filepath.Join(stateDir, "connection-health", "sealed")
}

// sealedMachinesFunc lists the account's machines with this machine's
// credential; tests replace it with a loopback server.
type sealedMachinesFunc func(ctx context.Context, apiBase, credential string) ([]adaptercloud.MachineRecord, error)

// sealedClient is the owner-sealed sender's own HTTPS client: its own
// transport and connection, never the relay's line, so a stuck relay
// socket, writer lock or spool cannot hold it. It follows no redirect,
// because a redirect would carry the bearer somewhere else.
func sealedClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: healthsealed.MaxAttemptSeconds * time.Second,
			MaxConnsPerHost:       1,
			MaxIdleConns:          1,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		Timeout:       healthsealed.MaxAttemptSeconds * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// sealedMachines is the production sealedMachinesFunc: one bounded GET
// /v1/machines on the sealed sender's own client.
func sealedMachines(ctx context.Context, apiBase, credential string) ([]adaptercloud.MachineRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, healthsealed.MaxAttemptSeconds*time.Second)
	defer cancel()
	c := &adaptercloud.AccountClient{BaseURL: strings.TrimRight(apiBase, "/"), HTTP: sealedClient()}
	return c.Machines(ctx, credential)
}

// sealedKeys is the daemon's healthsealed.KeySource. It reads the master
// secret once per seal and asks the content-key epoch record about that
// very secret (its key id, ContentEpochFile), so a secret replaced between
// two reads can never be sealed under the other's epoch. An epoch that is
// unknown or names another key or account is CodeKeyHandoverRequired, and
// it is checked before any request: no epoch is ever assumed. Only then is
// the account's current identity epoch asked of Cloud, on the sealed
// sender's own client.
type sealedKeys struct {
	stateDir string
	foreign  []string
	machines sealedMachinesFunc
}

// local reads, from local files only, the opt-in-independent half: Cloud's
// switch, the identity, and that both name the same control plane.
func (k sealedKeys) local() (adaptercloud.Identity, adaptercloud.Settings, error) {
	settings, err := adaptercloud.ReadSettings(nextconfig.Open(k.stateDir, k.foreign...))
	if err != nil || !settings.Enabled {
		return adaptercloud.Identity{}, adaptercloud.Settings{}, &healthsealed.Error{Code: healthsealed.CodeDisabled}
	}
	identity, found, err := adaptercloud.NewIdentityStore(filepath.Join(k.stateDir, cloudkeys.DirName)).Load()
	if err != nil || !found || !identity.Valid() || adaptercloud.CheckEnvironment(identity, settings) != nil {
		return adaptercloud.Identity{}, adaptercloud.Settings{}, &healthsealed.Error{Code: healthsealed.CodeKeyUnavailable}
	}
	return identity, settings, nil
}

// master answers the master secret with its recorded content-key epoch,
// or a typed error; the caller clears the secret.
func (k sealedKeys) master(accountID string) ([]byte, int64, error) {
	files, err := cloudkeys.Open(k.stateDir, k.foreign...)
	if err != nil {
		return nil, 0, &healthsealed.Error{Code: healthsealed.CodeKeyUnavailable}
	}
	key, found, err := files.MasterSecret()
	if err != nil || !found {
		return nil, 0, &healthsealed.Error{Code: healthsealed.CodeKeyUnavailable}
	}
	secret := key.Bytes()
	epoch, known, err := adaptercloud.ContentEpochStore{Dir: filepath.Join(k.stateDir, cloudkeys.DirName)}.Load(accountID, secret)
	if err != nil || !known || epoch < 1 {
		clear(secret)
		return nil, 0, &healthsealed.Error{Code: healthsealed.CodeKeyHandoverRequired}
	}
	return secret, epoch, nil
}

// Ready is the local-only half of Keys: no request is made.
func (k sealedKeys) Ready() error {
	identity, _, err := k.local()
	if err != nil {
		return err
	}
	secret, _, err := k.master(identity.AccountID)
	clear(secret)
	return err
}

func (k sealedKeys) Keys(ctx context.Context) (healthsealed.Keys, error) {
	identity, settings, err := k.local()
	if err != nil {
		return healthsealed.Keys{}, err
	}
	secret, epoch, err := k.master(identity.AccountID)
	if err != nil {
		return healthsealed.Keys{}, err
	}
	machines, err := k.machines(ctx, settings.APIBase, identity.MachineCredential)
	if err != nil {
		clear(secret)
		if errors.Is(err, adaptercloud.ErrUnauthorized) {
			return healthsealed.Keys{}, &healthsealed.Error{Code: healthsealed.CodeAuthRefused}
		}
		return healthsealed.Keys{}, &healthsealed.Error{Code: healthsealed.CodeUpstream}
	}
	for _, m := range machines {
		if m.ID != identity.MachineID {
			continue
		}
		if m.RevokedAt != nil || m.IdentityEpoch == nil || *m.IdentityEpoch < 0 {
			break
		}
		return healthsealed.Keys{Tenant: identity.AccountID, MachineID: identity.MachineID,
			Credential: identity.MachineCredential, MasterSecret: secret, ContentKeyEpoch: epoch,
			IdentityEpoch: int64(*m.IdentityEpoch)}, nil
	}
	clear(secret)
	return healthsealed.Keys{}, &healthsealed.Error{Code: healthsealed.CodeKeyUnavailable}
}

// newSealedDiagnostics makes the owner-sealed diagnostics worker. It is off
// unless the person opted in to owner retention (healthsealed.SettingKey,
// off by default) and Cloud is on; both are re-read before every seal and
// every send, so turning either off stops the next request.
func newSealedDiagnostics(stateDir string, foreign []string, machines sealedMachinesFunc) (*healthsealed.Worker, error) {
	store, err := healthsealed.OpenStore(connectionHealthSealedDir(stateDir))
	if err != nil {
		return nil, err
	}
	keys := sealedKeys{stateDir: stateDir, foreign: foreign, machines: machines}
	u := &healthsealed.Uploader{
		Client: sealedClient(),
		Origin: func() (string, error) {
			_, settings, err := keys.local()
			return settings.APIBase, err
		},
		Ready: keys.Ready,
		Enabled: func() bool {
			file := nextconfig.Open(stateDir, foreign...)
			values, err := file.Read()
			if err != nil || !healthsealed.OptedIn(values.Raw) {
				return false
			}
			settings, err := adaptercloud.ReadSettings(file)
			return err == nil && settings.Enabled
		},
		Keys:  keys,
		Store: store,
	}
	return healthsealed.NewWorker(u), nil
}

// sealedEvents are the records that say where the machine was stuck.
var sealedEvents = map[connectionhealth.Event]bool{
	connectionhealth.EventSlowDetected: true, connectionhealth.EventStallDetected: true,
	connectionhealth.EventReleasedStalled: true, connectionhealth.EventStateAfterLoss: true,
}

// sealedJob turns one stamped journal record into a snapshot for the
// owner, or ok=false for a record that says nothing about a stuck phase.
// The snapshot is the healthsealed whitelist only; its coverage names how
// many transitions the watchdog had dropped by then (lost), read from an
// atomic counter.
func sealedJob(r connectionhealth.Record, lost uint64) (healthsealed.Job, bool) {
	if !sealedEvents[r.Event] {
		return healthsealed.Job{}, false
	}
	if r.Event == connectionhealth.EventStateAfterLoss && r.State != connectionhealth.StateSlow && r.State != connectionhealth.StateBlocked {
		return healthsealed.Job{}, false
	}
	subject := r.EventID
	if r.PhaseID > 0 {
		subject = "phase-" + strconv.FormatUint(r.PhaseID, 10)
	}
	at := healthsealed.Int64{Value: r.Wall.UnixMilli(), Set: !r.Wall.IsZero()}
	snap := healthsealed.Snapshot{Schema: healthsealed.SnapshotSchema, Location: r.Location,
		AgeNS: healthsealed.Int64{Value: r.AgeNS, Set: true}, DeadlineNS: healthsealed.Int64{Value: r.DeadlineNS, Set: r.DeadlineNS > 0},
		Build: r.Build, Stage: string(r.Stage), CorrelationID: r.Correlation, ObservedAtMS: at,
		Coverage: []healthsealed.Coverage{{Source: "watchdog.transitions", FromMS: at, ToMS: at,
			Lost: healthsealed.Int64{Value: int64(min(lost, 1<<62)), Set: true}}},
		Lost: healthsealed.Int64{Value: int64(min(lost, 1<<62)), Set: true}}
	if snap.Validate() != nil {
		return healthsealed.Job{}, false
	}
	return healthsealed.Job{BootID: r.BootID, SubjectKind: healthsealed.SubjectIncident, SubjectID: subject, Snapshot: snap}, true
}

// stallCaptureEnv turns the stall capture worker off for a boot ("off").
// It is on by default: the measured cost is in
// docs/admin-connection-health.md "Stall capture", and a capture only runs
// when a phase is called blocked, at most MaxCapturesPerWindow a minute.
const stallCaptureEnv = "CLAWDLINE_STALL_CAPTURE"

func stallCaptureOn() bool {
	on := !strings.EqualFold(os.Getenv(stallCaptureEnv), "off")
	if !on {
		log.Printf("connection health: stall capture off (%s=off)", stallCaptureEnv)
	}
	return on
}

// sealedFeed is what the journal writer offers each record to. It reads
// only the worker's cached opt-in and an atomic counter, and Offer never
// waits, so the writer is never held by the owner-sealed sender. It also
// subscribes to the capture worker: a published capture becomes a /2
// revision of its stall's attachment, and so does the stall's recovery.
func sealedFeed(rt *connectionhealth.Runtime, w *healthsealed.Worker) func(connectionhealth.Record) {
	if w == nil {
		return nil
	}
	incidents := newOwnerIncidents()
	rt.Capture.OnPublish(func(c *connectionhealth.Capture) {
		if !w.Enabled() {
			return
		}
		if job, ok := incidents.captured(c); ok {
			_ = w.Offer(job)
		}
	})
	return func(r connectionhealth.Record) {
		if !w.Enabled() {
			return
		}
		_, lost := rt.Watchdog.Pending()
		if job, ok := sealedJob(r, lost); ok {
			incidents.remember(r, job)
			_ = w.Offer(job)
		}
		if job, ok := incidents.recovered(r); ok {
			_ = w.Offer(job)
		}
	}
}

// maxOwnerIncidents is how many stalled phases the owner feed remembers so
// a capture or a recovery can revise the stall's attachment (capacity row
// cloud.health_owner_incidents). The oldest is forgotten; its later
// capture is then sealed from the capture's own trigger, and its recovery
// is not sealed, which the local export still shows.
const maxOwnerIncidents = 32

// ownerIncidents is the owner feed's memory of stalls. Its lock is held only
// for a map update by the journal writer or the capture worker, never by
// business code, and nothing under it waits.
type ownerIncidents struct {
	mu    sync.Mutex
	rows  map[uint64]*ownerIncident
	order []uint64
}

type ownerIncident struct {
	job       healthsealed.Job
	stallMono int64
	capture   *healthsealed.CaptureExcerpt
	recovery  *healthsealed.Recovery
}

func newOwnerIncidents() *ownerIncidents {
	return &ownerIncidents{rows: map[uint64]*ownerIncident{}}
}

// remember keeps a stall's job as the base of its later revisions.
func (o *ownerIncidents) remember(r connectionhealth.Record, job healthsealed.Job) {
	if r.Event != connectionhealth.EventStallDetected || r.PhaseID == 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if row, ok := o.rows[r.PhaseID]; ok {
		row.job, row.stallMono = job, r.MonoNS
		return
	}
	if len(o.order) >= maxOwnerIncidents {
		delete(o.rows, o.order[0])
		o.order = o.order[1:]
	}
	o.rows[r.PhaseID] = &ownerIncident{job: job, stallMono: r.MonoNS}
	o.order = append(o.order, r.PhaseID)
}

// recovered is the /2 revision for a remembered stall that recovered or
// was released.
func (o *ownerIncidents) recovered(r connectionhealth.Record) (healthsealed.Job, bool) {
	var rec healthsealed.Recovery
	switch r.Event {
	case connectionhealth.EventRecovered:
		rec.State = connectionhealth.RecoveryRecovered
	case connectionhealth.EventReleasedStalled:
		rec.State = connectionhealth.RecoveryReleased
	default:
		return healthsealed.Job{}, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	row, ok := o.rows[r.PhaseID]
	if !ok || row.recovery != nil {
		return healthsealed.Job{}, false
	}
	if rec.State == connectionhealth.RecoveryRecovered && r.MonoNS >= row.stallMono {
		rec.RecoveryNS = healthsealed.Int64{Value: r.MonoNS - row.stallMono, Set: true}
	}
	row.recovery = &rec
	return row.revision()
}

// captured is the /2 revision carrying a published capture.
func (o *ownerIncidents) captured(c *connectionhealth.Capture) (healthsealed.Job, bool) {
	ex := captureExcerpt(c)
	o.mu.Lock()
	defer o.mu.Unlock()
	row, ok := o.rows[c.Trigger.PhaseID]
	if !ok {
		t := c.Trigger
		at := healthsealed.Int64{Value: t.RequestedWall.UnixMilli(), Set: !t.RequestedWall.IsZero()}
		row = &ownerIncident{job: healthsealed.Job{BootID: c.BootID, SubjectKind: healthsealed.SubjectIncident,
			SubjectID: "phase-" + strconv.FormatUint(t.PhaseID, 10),
			Snapshot: healthsealed.Snapshot{Schema: healthsealed.SnapshotSchema, Location: t.Location,
				AgeNS: healthsealed.Int64{Value: t.AgeNS, Set: true}, DeadlineNS: healthsealed.Int64{Value: t.DeadlineNS, Set: t.DeadlineNS > 0},
				Build: c.Build, Stage: string(t.Stage), CorrelationID: t.Correlation, ObservedAtMS: at}}}
	}
	row.capture = ex
	if row.job.Snapshot.Build == "" {
		// Journal records carry the build only on the boot record; the
		// capture names the build its source lines are exact for.
		row.job.Snapshot.Build = c.Build
	}
	return row.revision()
}

// revision is the remembered stall with its capture and recovery, as /2,
// trimmed frame by frame until it fits MaxSnapshotBytes.
func (row *ownerIncident) revision() (healthsealed.Job, bool) {
	job := row.job
	job.Snapshot.Schema = healthsealed.SnapshotSchemaV2
	job.Snapshot.Recovery = row.recovery
	if row.capture != nil {
		ex := *row.capture
		ex.Frames = append([]healthsealed.CaptureFrame(nil), ex.Frames...)
		job.Snapshot.Capture = &ex
	}
	for {
		_, err := job.Snapshot.Encode()
		if err == nil {
			return job, true
		}
		c := job.Snapshot.Capture
		if c == nil || len(c.Frames) == 0 {
			return healthsealed.Job{}, false
		}
		c.Frames = c.Frames[:len(c.Frames)-1]
		c.FramesOmitted.Value++
		c.Truncated = true
	}
}

// captureExcerpt is the owner's part of a capture: the join and the first
// matching goroutine's frames, never the other goroutines.
func captureExcerpt(c *connectionhealth.Capture) *healthsealed.CaptureExcerpt {
	n := func(v int) healthsealed.Int64 { return healthsealed.Int64{Value: int64(v), Set: true} }
	ex := &healthsealed.CaptureExcerpt{Seq: healthsealed.Int64{Value: int64(min(c.Seq, 1<<62)), Set: true},
		DurationNS: healthsealed.Int64{Value: c.DurationNS, Set: true}, OverBudget: c.OverBudget,
		Function: c.Join.Function, Mapped: c.Join.Mapped, MatchedGoroutines: n(c.Join.Goroutines),
		GoroutinesTotal: n(c.GoroutinesTotal), RawTruncated: c.RawTruncated, Truncated: c.Truncated,
		Redacted: n(c.Redacted), SourceUnavailable: n(c.SourceUnavailable), FramesOmitted: n(0)}
	ex.Sources = append(ex.Sources, c.Join.Sources[:min(len(c.Join.Sources), healthsealed.MaxSnapshotSources)]...)
	if len(c.Join.Sources) > healthsealed.MaxSnapshotSources {
		ex.Truncated = true
	}
	if len(c.Goroutines) > 0 && c.Goroutines[0].MatchesLocation {
		g := c.Goroutines[0]
		ex.State = g.State
		keep := min(len(g.Frames), healthsealed.MaxSnapshotFrames)
		for _, f := range g.Frames[:keep] {
			ex.Frames = append(ex.Frames, healthsealed.CaptureFrame{Function: f.Function, Source: f.Source, SourceKind: f.SourceKind})
		}
		ex.FramesOmitted = n(g.FramesOmitted + len(g.Frames) - keep)
		if keep < len(g.Frames) {
			ex.Truncated = true
		}
	}
	return ex
}

// ownerDiagnosticsStatus is the sealed sender as the health status shows
// it, with a typed coverage: off, waiting (on, nothing to seal yet),
// active, partial (jobs dropped or refused), or failed with its code.
func ownerDiagnosticsStatus(w *healthsealed.Worker, openErr error) func() *healthupload.OwnerDiagnostics {
	return func() *healthupload.OwnerDiagnostics {
		if w == nil {
			code := healthsealed.CodeOf(openErr)
			if code == "" {
				code = healthsealed.CodeStoreUnwritable
			}
			return &healthupload.OwnerDiagnostics{Coverage: "failed", Code: string(code)}
		}
		st := w.Status()
		d := &healthupload.OwnerDiagnostics{Code: string(st.Code), Cloud: st.Cloud, Stored: st.Stored,
			Refused: st.Refused, Dropped: st.Dropped, LastAtMS: st.LastAtMS, Queued: st.Queued}
		if st.StoreMeasured {
			n, b := st.Attachments, st.StoreBytes
			d.Attachments, d.StoreBytes = &n, &b
		}
		switch {
		case !st.Enabled:
			d.Coverage = "off"
		case st.Ready != "":
			d.Coverage, d.Code = "failed", string(st.Ready)
		case st.Code != "":
			d.Coverage = "failed"
		case st.Dropped > 0 || st.Refused > 0:
			d.Coverage = "partial"
		case st.Stored > 0:
			d.Coverage = "active"
		default:
			d.Coverage = "waiting"
		}
		return d
	}
}

// healthTimeout bounds one CLI read. The route reads memory only, so a
// daemon that takes longer than this is itself the finding.
const healthTimeout = 10 * time.Second

// healthCommand is `clawdline health status|events|trace|export`: the
// daemon's connection-health watchdog and journal, read over loopback with
// this machine's own token. It never borrows a browser's credential and
// never asks Cloud.
func healthCommand(args []string) {
	cfg := config.Load()
	port := 0
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port, _ = strconv.Atoi(args[i+1])
		}
	}
	if port == 0 {
		p, err := daemonPort()
		if err != nil {
			fail(err)
		}
		port = p
	}
	client := &http.Client{Timeout: healthTimeout}
	newRequest := func(path string, q url.Values) (*http.Request, error) {
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		return daemonRequestWithConfig(http.MethodGet, path, nil, cfg, port)
	}
	os.Exit(healthRun(os.Stdout, os.Stderr, args, time.Now(), newRequest, client.Do))
}

const healthUsage = "usage: clawdline health <status|events|trace|export|history> [--since T] [--until T] [--after-seq N] [--limit N] [--boot-id ID] [--correlation ID] [--cursor C] [--json] [--port n]\n" +
	"  T is RFC 3339 (2026-10-20T12:00:00Z) or a duration back from now (15m)\n" +
	"  events/export read this boot's memory; history reads the durable store across boots (--cursor v1.<segment>.<offset>)"

// healthRun is the command without the process around it, for tests. It
// returns the exit status: 0 answered, 1 refused or unreachable, 2 usage.
func healthRun(stdout, stderr io.Writer, args []string, now time.Time,
	newRequest func(string, url.Values) (*http.Request, error),
	do func(*http.Request) (*http.Response, error)) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	sub := args[0]
	fs := flag.NewFlagSet("health "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	since := fs.String("since", "", "")
	until := fs.String("until", "", "")
	after := fs.String("after-seq", "", "")
	limit := fs.Int("limit", 0, "")
	boot := fs.String("boot-id", "", "")
	correlation := fs.String("correlation", "", "")
	cursor := fs.String("cursor", "", "")
	asJSON := fs.Bool("json", false, "")
	_ = fs.Int("port", 0, "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	q := url.Values{}
	switch sub {
	case "status":
	case "events", "export":
		for _, f := range []struct{ name, value string }{{"since", *since}, {"until", *until}} {
			if f.value == "" {
				continue
			}
			t, err := healthTime(f.value, now)
			if err != nil {
				fmt.Fprintf(stderr, "clawdline health: --%s: %v\n", f.name, err)
				return 2
			}
			q.Set(f.name, t.UTC().Format(time.RFC3339Nano))
		}
		if *after != "" {
			// Checked here as well as by the daemon, so a cursor that does
			// not fit a sequence number is refused before any request.
			if _, err := strconv.ParseUint(*after, 10, 64); err != nil {
				fmt.Fprintln(stderr, "clawdline health: --after-seq is not a sequence number (invalid_cursor)")
				return 2
			}
			q.Set("after_seq", *after)
		}
		if *limit < 0 || *limit > connectionhealth.MaxQueryRecords {
			fmt.Fprintf(stderr, "clawdline health: --limit must be 0..%d (query_bound_exceeded)\n", connectionhealth.MaxQueryRecords)
			return 2
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		if *boot != "" {
			q.Set("boot_id", *boot)
		}
	case "history":
		if *limit < 0 || *limit > connectionhealth.MaxQueryRecords {
			fmt.Fprintf(stderr, "clawdline health: --limit must be 0..%d (query_bound_exceeded)\n", connectionhealth.MaxQueryRecords)
			return 2
		}
		if *limit > 0 {
			q.Set("limit", strconv.Itoa(*limit))
		}
		if *cursor != "" {
			q.Set("cursor", *cursor)
		}
	case "trace":
		if !connectionhealth.ValidID(*correlation) {
			fmt.Fprintln(stderr, "clawdline health trace: --correlation must be an opaque id such as op-1k (invalid_field)")
			return 2
		}
		q.Set("correlation", *correlation)
	default:
		fmt.Fprintln(stderr, healthUsage)
		return 2
	}
	req, err := newRequest("/v1/connection-health/"+sub, q)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline health: %v\n", err)
		return 1
	}
	res, err := do(req)
	if err != nil {
		// Unreachable is not "healthy" and not "no events": it is unknown.
		fmt.Fprintf(stderr, "clawdline health: the daemon could not be asked; connection health is unknown: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, connectionhealth.MaxQueryBytes+connectionhealth.MaxJournalBytes))
	if err != nil {
		fmt.Fprintf(stderr, "clawdline health: the answer could not be read; connection health is unknown: %v\n", err)
		return 1
	}
	if res.StatusCode != http.StatusOK {
		// The daemon's one refusal shape: `error` is the code to branch on.
		var refusal struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(body, &refusal)
		if refusal.Error == "" {
			refusal.Error = strconv.Itoa(res.StatusCode)
		}
		fmt.Fprintf(stderr, "clawdline health %s: refused, %d %s: %s\n", sub, res.StatusCode, refusal.Error, strings.TrimSpace(refusal.Detail))
		return 1
	}
	if sub == "status" && !*asJSON {
		return healthSummary(stdout, stderr, body)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") != nil {
		fmt.Fprintln(stderr, "clawdline health: the daemon's answer is not JSON; connection health is unknown")
		return 1
	}
	pretty.WriteByte('\n')
	_, _ = stdout.Write(pretty.Bytes())
	return 0
}

// healthTime reads an RFC 3339 instant or a duration back from now.
func healthTime(v string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return time.Time{}, errors.New("not an RFC 3339 time or a positive duration")
	}
	return now.Add(-d), nil
}

// healthSummary prints the status answer as a few lines: the quality first,
// so "could not see" is never read as "fine", then each finding that is not
// idle or progressing.
func healthSummary(stdout, stderr io.Writer, body []byte) int {
	var st connectionhealth.Status
	var durable struct {
		Durable *struct {
			State  string `json:"state"`
			Code   string `json:"code"`
			Status *struct {
				State   string `json:"state"`
				Records uint64 `json:"records"`
				Synced  uint64 `json:"synced_through"`
				Lost    uint64 `json:"lost_records"`
			} `json:"status"`
		} `json:"durable"`
		Upload *struct {
			State  string               `json:"state"`
			Status *healthupload.Status `json:"status"`
		} `json:"upload"`
	}
	_ = json.Unmarshal(body, &durable)
	if json.Unmarshal(body, &st) != nil || st.Schema != connectionhealth.StatusSchema {
		fmt.Fprintln(stderr, "clawdline health: the daemon's answer is not a connection-health status; it is unknown")
		return 1
	}
	fmt.Fprintf(stdout, "boot %s  build %s  quality %s  running %t\n", st.BootID, st.Build, st.Quality, st.Running)
	switch d := durable.Durable; {
	case d == nil:
		fmt.Fprintln(stdout, "durable store: unknown (not in this daemon's answer)")
	case d.Status != nil:
		fmt.Fprintf(stdout, "durable store: %s  %s  records %d  synced through ticket %d  lost %d\n",
			d.State, d.Status.State, d.Status.Records, d.Status.Synced, d.Status.Lost)
	case d.Code != "":
		fmt.Fprintf(stdout, "durable store: %s (%s); this boot is memory only\n", d.State, d.Code)
	default:
		fmt.Fprintf(stdout, "durable store: %s\n", d.State)
	}
	switch u := durable.Upload; {
	case u == nil:
		fmt.Fprintln(stdout, "cloud upload: unknown (not in this daemon's answer)")
	case u.Status == nil:
		fmt.Fprintf(stdout, "cloud upload: %s\n", u.State)
	default:
		t := u.Status.Totals
		pending := "none"
		if p := u.Status.Pending; p != nil {
			pending = fmt.Sprintf("%s (%d events, %d attempts, persisted %s)", p.BatchID, p.Events, p.Attempts, p.Persisted)
		}
		fmt.Fprintf(stdout, "cloud upload: %s  receipted %d  pending %s  refused %d  abandoned %d  this hour %d B / %d requests\n",
			u.Status.State, t.Receipted, pending, t.RefusedEvents, t.AbandonedEvents, u.Status.Hour.Bytes, u.Status.Hour.Requests)
	}
	if st.Report == nil {
		fmt.Fprintln(stdout, "watchdog: no scan yet (not_collected)")
		return 0
	}
	r := st.Report
	fmt.Fprintf(stdout, "watchdog: scan %d  trace coverage %s  transitions %s (lost %d)  overflow %d (critical %d)\n",
		r.ScanSeq, r.TraceCoverage, r.Transitions, r.TransitionsLost, r.Overflow.Total, r.Overflow.Critical)
	fmt.Fprintf(stdout, "journal: seq %d..%d  evicted %d  refused %d  %s\n",
		st.Coverage.FirstRetainedSeq, st.Coverage.LastSeq, st.Coverage.Evicted, st.Coverage.Refused, st.Coverage.Durability)
	shown := 0
	for _, f := range r.Findings {
		if f.State == connectionhealth.StateIdle || f.State == connectionhealth.StateProgressing {
			continue
		}
		shown++
		fmt.Fprintf(stdout, "  %-22s %-20s %-36s age %s  %s\n", f.State, f.Stage, f.Location,
			time.Duration(f.AgeNS).Round(time.Millisecond), f.Correlation)
	}
	if shown == 0 {
		fmt.Fprintf(stdout, "  %d active phases, none slow or blocked\n", len(r.Findings))
	}
	return 0
}
