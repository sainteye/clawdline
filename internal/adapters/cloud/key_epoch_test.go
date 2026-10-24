package cloud

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

// countingPeer reads what the transport writes, once gate opens, and counts
// the bytes.
func countingPeer(peer net.Conn, gate <-chan struct{}, read *atomic.Int64) {
	<-gate
	buf := make([]byte, 64<<10)
	for {
		n, err := peer.Read(buf)
		read.Add(int64(n))
		if err != nil {
			return
		}
	}
}

func epochKeys(t *testing.T) (*domain.KeyEpoch, domain.ContentKeyState) {
	t.Helper()
	key, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first := domain.ContentKeyState{Epoch: 1, KeyID: MasterKeyID, Key: key}
	keys, err := domain.NewKeyEpoch(first)
	if err != nil {
		t.Fatal(err)
	}
	return keys, first
}

func nextEpoch(t *testing.T, epoch uint64) domain.ContentKeyState {
	t.Helper()
	key, err := domain.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return domain.ContentKeyState{Epoch: epoch, KeyID: fmt.Sprintf("mk-%d-%x", epoch, sha256.Sum256(key.Bytes())), Key: key}
}

func labelledRow(t *testing.T, s *Spool, recipient, keyID string) uint64 {
	t.Helper()
	seq, err := s.Reserve(SpoolChannelT, recipient, recipient, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LabelKey(seq, keyID); err != nil {
		t.Fatal(err)
	}
	if err := s.Seal(seq, []byte(`{"frame":"`+recipient+`"}`), s.now()); err != nil {
		t.Fatal(err)
	}
	return seq
}

// A writer that has taken an old-key row and is blocked on the socket holds
// the cutover back: the cutover cannot report success while bytes sealed
// under the key it retires are still on their way out. Once it has returned,
// nothing under the old key is written again — not the next ready row, and
// not a reconnect's re-send of a row that was already sent.
func TestACutoverWaitsForAnOldRowsWriteAndNothingOldIsWrittenAfter(t *testing.T) {
	keys, first := epochKeys(t)
	s, err := NewSpool(DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	blocked := labelledRow(t, s, "t/m/blocked", first.KeyID)
	behind := labelledRow(t, s, "t/m/behind", first.KeyID)
	var settled []uint64
	transport := &Transport{opts: Options{Spool: s, KeyEpoch: keys, Now: time.Now,
		Status: NewStatusRecorder(time.Now()), Log: func(string) {},
		OnSettled: func(_ string, seq uint64, kind SettleKind) {
			if kind == SettleBurned {
				settled = append(settled, seq)
			}
		}}}
	conn, peer := pipeConn(nil)
	gate := make(chan struct{})
	// Observe complete frames and join the reader before inspecting them.
	// net.Pipe.Write can finish before the reader records the payload; a
	// nonzero byte count may describe only the WebSocket header.
	var frames [][]byte
	var readErr error
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		<-gate
		reader := bufio.NewReader(peer)
		for {
			frame, err := readServerFrame(reader)
			if err != nil {
				readErr = err
				return
			}
			frames = append(frames, frame)
		}
	}()
	t.Cleanup(func() { _ = conn.Close(); _ = peer.Close() })

	drained := make(chan bool)
	go func() { drained <- transport.drainOnce(conn) }()
	waitFor(t, 5*time.Second, func() bool { row, _ := s.Row(blocked); return row.State == SpoolSent })

	cutover := make(chan error)
	go func() {
		_, err := keys.Cutover(func(domain.ContentKeyState) (domain.ContentKeyState, error) {
			return nextEpoch(t, 2), nil
		}, s.RetireForCutover)
		cutover <- err
	}()
	select {
	case err := <-cutover:
		t.Fatalf("the cutover returned (%v) while an old row's write was still blocked", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(gate)
	if err := <-cutover; err != nil {
		t.Fatal(err)
	}
	if !<-drained {
		t.Fatal("the drain failed")
	}
	for _, seq := range []uint64{blocked, behind} {
		if row, _ := s.Row(seq); row.State != SpoolBurned || row.BurnReason != BurnKeyRetired {
			t.Fatalf("seq %d after the cutover: %+v", seq, row)
		}
	}

	// A row the retire missed is still never written: the sender checks it
	// against the installed key under the same fence.
	missed := labelledRow(t, s, "t/m/missed", first.KeyID)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() { transport.drain(conn, stop, true); close(finished) }()
	waitFor(t, 5*time.Second, func() bool { row, _ := s.Row(missed); return row.State == SpoolBurned })
	close(stop)
	<-finished
	_ = conn.Close()
	<-readerDone
	if !errors.Is(readErr, io.EOF) {
		t.Fatalf("reading the complete publication stream: %v", readErr)
	}
	want := PublishFrame([]byte(`{"frame":"t/m/blocked"}`))
	if len(frames) != 1 || !bytes.Equal(frames[0], want) {
		t.Fatalf("publications across cutover: %q, want only %q", frames, want)
	}
	if row, _ := s.Row(missed); row.BurnReason != BurnKeyRetired {
		t.Fatalf("the missed row: %+v", row)
	}
	if len(settled) != 3 {
		t.Fatalf("burned rows told to their waiters: %v, want 3", settled)
	}
	_ = peer.Close()
}

// A reconnect's re-send that has taken its list of sent rows, and is blocked
// writing the first, holds the cutover too; the rows behind it in the list
// were retired by the time it reaches them and are skipped, not written.
func TestAReconnectResendCannotOutliveTheCutoverOfItsRows(t *testing.T) {
	keys, first := epochKeys(t)
	s, err := NewSpool(DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	a := labelledRow(t, s, "t/m/a", first.KeyID)
	b := labelledRow(t, s, "t/m/b", first.KeyID)
	for range 2 {
		if d := s.SendNext(); d.Row == nil {
			t.Fatalf("not sent: %+v", d)
		}
	}
	transport := &Transport{opts: Options{Spool: s, KeyEpoch: keys, Now: time.Now,
		Status: NewStatusRecorder(time.Now()), Log: func(string) {}}}
	conn, peer := pipeConn(nil)
	gate := make(chan struct{})
	var read atomic.Int64
	go countingPeer(peer, gate, &read)
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() { transport.drain(conn, stop, true); close(finished) }()
	time.Sleep(50 * time.Millisecond) // the re-send of a is now blocked on the socket

	cutover := make(chan error)
	go func() {
		_, err := keys.Cutover(func(domain.ContentKeyState) (domain.ContentKeyState, error) {
			return nextEpoch(t, 2), nil
		}, s.RetireForCutover)
		cutover <- err
	}()
	select {
	case err := <-cutover:
		t.Fatalf("the cutover returned (%v) under a blocked re-send", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(gate)
	if err := <-cutover; err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-finished
	frame := int64(len(PublishFrame([]byte(`{"frame":"t/m/a"}`))))
	if got := read.Load(); got > frame+16 {
		t.Fatalf("%d bytes were written; only the re-send of %d (about %d) was owed", got, a, frame)
	}
	if row, _ := s.Row(b); row.State != SpoolBurned {
		t.Fatalf("seq %d after the cutover: %+v", b, row)
	}
	_ = peer.Close()
}

// The inbound ladder opens an ordinary request only under the installed key
// and only when it names the installed id: the old key is refused once the
// cutover returns, a frame claiming the new id under the old key does not
// open, and the legitimate new key does.
func TestInboundOpensOnlyUnderTheInstalledContentKey(t *testing.T) {
	for _, channel := range []string{"ctl/machine", "r/machine", "termi/machine/viewer"} {
		t.Run(channel, func(t *testing.T) {
			keys, first := epochKeys(t)
			viewer, err := domain.NewDeviceKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			admitted := 0
			tr := &Transport{opts: Options{
				Identity:     Identity{MachineID: "machine"},
				Status:       NewStatusRecorder(time.Now()),
				Replay:       domain.NewReplayWindow(64),
				ContentKey:   first.Key, // the legacy field is ignored once KeyEpoch is set
				KeyEpoch:     keys,
				PublicKeyFor: func(sender string) (ed25519.PublicKey, bool) { return viewer.PublicKey(), sender == "viewer" },
				Inbound:      func(domain.Envelope, []byte, ed25519.PublicKey) { admitted++ },
			}}
			seq := uint64(0)
			seal := func(keyID string, key domain.ContentKey) []byte {
				t.Helper()
				seq++
				env, err := domain.Seal([]byte(`{"v":1}`), domain.SealParams{Ch: channel, Seq: seq,
					Ts: uint64(time.Now().UnixMilli()), Class: domain.ClassCtl, KeyID: keyID, Sender: "viewer",
					Key: key, Signer: viewer})
				if err != nil {
					t.Fatal(err)
				}
				raw, err := env.CanonicalJSON()
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			if !tr.admitEnvelope(seal(first.KeyID, first.Key), "") {
				t.Fatal("the installed key was refused")
			}
			next := nextEpoch(t, 2)
			if _, err := keys.Cutover(func(domain.ContentKeyState) (domain.ContentKeyState, error) { return next, nil },
				func(_, _ domain.ContentKeyState) error { return nil }); err != nil {
				t.Fatal(err)
			}
			cases := map[string]struct {
				id  string
				key domain.ContentKey
			}{
				"the retired key and id":           {first.KeyID, first.Key},
				"the new id under the old key":     {next.KeyID, first.Key},
				"the old id under the new key":     {first.KeyID, next.Key},
				"a connection key id on a request": {"rk-aaaaaaaaaaaaaaaaaaaaaa", next.Key},
			}
			for name, c := range cases {
				if tr.admitEnvelope(seal(c.id, c.key), "") {
					t.Errorf("%s: admitted", name)
				}
			}
			if !tr.admitEnvelope(seal(next.KeyID, next.Key), "") || admitted != 2 {
				t.Fatalf("the new key's canary: admitted %d, want 2", admitted)
			}
			if channel != "ctl/machine" {
				for name, c := range cases {
					if tr.AcceptDirect("viewer", seal(c.id, c.key)) {
						t.Errorf("direct %s: admitted", name)
					}
				}
				raw := seal(next.KeyID, next.Key)
				if !tr.AcceptDirect("viewer", raw) || admitted != 3 {
					t.Fatalf("the new key's direct canary: admitted %d, want 3", admitted)
				}
				if tr.admitEnvelope(raw, "") {
					t.Fatal("a direct request was replayed on the relay")
				}
			}

		})
	}
}

// The durable rotation writes the new epoch and its floor and then the
// callback fails — the reply lost, the disk erroring after the rename. The
// epoch halts: nothing is sent or opened under the old key, though the store
// may already be past it. Recovery adopts what the store holds, retires the
// old rows, and never moves the epoch back.
func TestAFailedRotationStopsSendingAndRecoveryNeverMovesTheEpochBack(t *testing.T) {
	store, err := cloudkeys.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.LoadOrCreateContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := domain.NewKeyEpoch(first)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSpool(DefaultSpoolLimits(), nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	old := labelledRow(t, s, "t/m/old", first.KeyID)
	lost := errors.New("the reply was lost after the floor moved")
	if _, err := keys.Cutover(func(state domain.ContentKeyState) (domain.ContentKeyState, error) {
		if _, err := store.RotateContentKey("op-1", state.Epoch, rand.Reader); err != nil {
			t.Fatal(err)
		}
		return domain.ContentKeyState{}, lost
	}, s.RetireForCutover); !errors.Is(err, domain.ErrKeyEpochHalted) || !errors.Is(err, lost) {
		t.Fatalf("the failed cutover answered %v", err)
	}

	transport := &Transport{opts: Options{Spool: s, KeyEpoch: keys, Now: time.Now,
		Status: NewStatusRecorder(time.Now()), Log: func(string) {}}}
	conn, peer := pipeConn(nil)
	open := make(chan struct{})
	close(open)
	var read atomic.Int64
	go countingPeer(peer, open, &read)
	if !transport.drainOnce(conn) {
		t.Fatal("a halted drain closed the line")
	}
	if row, _ := s.Row(old); row.State != SpoolReady || read.Load() != 0 {
		t.Fatalf("a halted epoch wrote %d bytes; the old row is %s", read.Load(), row.State)
	}
	if _, err := keys.Cutover(func(domain.ContentKeyState) (domain.ContentKeyState, error) {
		t.Fatal("a halted epoch rotated again")
		return domain.ContentKeyState{}, nil
	}, s.RetireForCutover); !errors.Is(err, domain.ErrKeyEpochHalted) {
		t.Fatalf("a second cutover on a halted epoch: %v", err)
	}

	// Recovery reads the store, which is past epoch 1; afterwards a recovery
	// that answers epoch 1 is a regression and halts again.
	if _, err := keys.Recover(store.RecoverContentKey, s.RetireForCutover); err != nil {
		t.Fatal(err)
	}
	current, err := keys.Current()
	if err != nil || current.Epoch != 2 {
		t.Fatalf("after recovery: %+v %v", current, err)
	}
	if _, err := keys.Recover(func() (domain.ContentKeyState, error) { return first, nil }, s.RetireForCutover); !errors.Is(err, domain.ErrKeyEpochRegress) {
		t.Fatalf("a recovery back to epoch 1 answered %v", err)
	}
	if _, err := keys.Current(); !errors.Is(err, domain.ErrKeyEpochHalted) {
		t.Fatal("a regressing recovery left the epoch open")
	}
	if _, err := keys.Recover(store.RecoverContentKey, s.RetireForCutover); err != nil {
		t.Fatal(err)
	}
	if row, _ := s.Row(old); row.State != SpoolBurned || row.BurnReason != BurnKeyRetired {
		t.Fatalf("the old row after recovery: %+v", row)
	}
	if !transport.drainOnce(conn) {
		t.Fatal("the recovered drain failed")
	}
	if read.Load() != 0 {
		t.Fatalf("the recovered drain wrote %d old bytes", read.Load())
	}
	_ = peer.Close()
}
