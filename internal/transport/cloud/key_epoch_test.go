package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

func epochState(t *testing.T, epoch uint64) domaincloud.ContentKeyState {
	t.Helper()
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := "ms-1"
	if epoch > 1 {
		id = fmt.Sprintf("mk-%d-%x", epoch, sha256.Sum256(key.Bytes()))
	}
	return domaincloud.ContentKeyState{Epoch: epoch, KeyID: id, Key: key}
}

func epochRelay(t *testing.T, keys *domaincloud.KeyEpoch) (*Relay, *adaptercloud.Spool, domaincloud.DeviceKey) {
	t.Helper()
	limits := adaptercloud.DefaultSpoolLimits()
	limits.RecipientRowCap = 10_000
	limits.GlobalRowCap = 10_000
	spool, err := adaptercloud.NewSpool(limits, nil, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &Relay{Transport: &adaptercloud.Transport{}, Spool: spool, MachineID: "mac-01",
		Signer: signer, Keys: keys}, spool, signer
}

// Many answers sealed while the key is cut over again and again: every row
// still owed names the key it opens under, and every row sealed under a key
// a cutover retired is burned rather than left to be written. A Relay that
// read the key and its id outside the fence would seal some rows under one
// epoch and label them with another, or queue one after its epoch retired.
func TestPublishUnderACutoverNeverMixesAKeyWithAnotherEpochsID(t *testing.T) {
	first := epochState(t, 1)
	keys, err := domaincloud.NewKeyEpoch(first)
	if err != nil {
		t.Fatal(err)
	}
	relay, spool, signer := epochRelay(t, keys)
	var mu sync.Mutex
	byID := map[string]domaincloud.ContentKey{first.KeyID: first.Key}
	stop := make(chan struct{})
	var cutovers sync.WaitGroup
	cutovers.Add(1)
	go func() {
		defer cutovers.Done()
		for epoch := uint64(2); ; epoch++ {
			select {
			case <-stop:
				return
			default:
			}
			next := epochState(t, epoch)
			mu.Lock()
			byID[next.KeyID] = next.Key
			mu.Unlock()
			if _, err := keys.Cutover(func(domaincloud.ContentKeyState) (domaincloud.ContentKeyState, error) {
				return next, nil
			}, spool.RetireForCutover); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	var publishers sync.WaitGroup
	for w := range 4 {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			for i := range 150 {
				if err := relay.Publish(context.Background(), Outbound{Channel: fmt.Sprintf("t/mac-01/s%d", w),
					Payload: []byte(fmt.Sprintf(`{"n":%d}`, i))}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	publishers.Wait()
	close(stop)
	cutovers.Wait()

	current, err := keys.Current()
	if err != nil {
		t.Fatal(err)
	}
	pinned := func(string) (ed25519.PublicKey, bool) { return signer.PublicKey(), true }
	live, retired := 0, 0
	for seq := range spool.NextSequence() {
		row, ok := spool.Row(seq)
		if !ok {
			continue
		}
		if row.State == adaptercloud.SpoolBurned {
			if row.BurnReason != adaptercloud.BurnKeyRetired || row.KeyID == current.KeyID {
				t.Fatalf("seq %d burned as %q under %s (current %s)", seq, row.BurnReason, row.KeyID, current.KeyID)
			}
			retired++
			continue
		}
		live++
		if row.KeyID != current.KeyID {
			t.Fatalf("seq %d is still %s under %s after the cutover to %s", seq, row.State, row.KeyID, current.KeyID)
		}
		env, err := domaincloud.DecodeEnvelope(row.Sealed)
		if err != nil {
			t.Fatal(err)
		}
		if env.KeyID != row.KeyID {
			t.Fatalf("seq %d is labelled %s and sealed as %s", seq, row.KeyID, env.KeyID)
		}
		if _, err := env.Open(byID[env.KeyID], pinned); err != nil {
			t.Fatalf("seq %d does not open under the key its id names: %v", seq, err)
		}
	}
	if live+retired != 600 || current.Epoch < 3 {
		t.Fatalf("live %d + retired %d of 600, at epoch %d", live, retired, current.Epoch)
	}
}

// A cutover retires every master-key row owed — reserved, ready and sent —
// and leaves a terminal reply's rk-* row alone: that key is the connection's,
// not this authority's, and burning it would drop a screen for no reason.
func TestACutoverRetiresMasterRowsAndKeepsTerminalReplies(t *testing.T) {
	first := epochState(t, 1)
	keys, err := domaincloud.NewKeyEpoch(first)
	if err != nil {
		t.Fatal(err)
	}
	relay, spool, _ := epochRelay(t, keys)
	publish := func(out Outbound) uint64 {
		t.Helper()
		seq, err := relay.PublishTracked(context.Background(), out)
		if err != nil {
			t.Fatal(err)
		}
		return seq
	}
	sent := publish(Outbound{Channel: "t/mac-01/sent", Payload: []byte(`{}`)})
	if d := spool.SendNext(); d.Row == nil || d.Row.Seq != sent {
		t.Fatalf("the first row did not go out: %+v", d)
	}
	ready := publish(Outbound{Channel: "t/mac-01/ready", Payload: []byte(`{}`)})
	reserved, err := spool.Reserve(adaptercloud.SpoolChannelT, "t/mac-01/reserved", "x", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.LabelKey(reserved, first.KeyID); err != nil {
		t.Fatal(err)
	}
	replyKey, _ := domaincloud.NewContentKey(rand.Reader)
	reply := publish(Outbound{Channel: "termr/mac-01/viewer/conn", Payload: []byte(`{}`),
		Key: replyKey, KeyID: "rk-" + strings.Repeat("a", 22)})

	if _, err := keys.Cutover(func(domaincloud.ContentKeyState) (domaincloud.ContentKeyState, error) {
		return epochState(t, 2), nil
	}, spool.RetireForCutover); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []uint64{sent, ready, reserved} {
		row, _ := spool.Row(seq)
		if row.State != adaptercloud.SpoolBurned || row.BurnReason != adaptercloud.BurnKeyRetired || row.Sealed != nil {
			t.Fatalf("seq %d was not retired: %+v", seq, row)
		}
	}
	if row, _ := spool.Row(sent); row.Outcome != adaptercloud.AttemptTransportUncertain {
		t.Fatalf("a retired sent row claims %q", row.Outcome)
	}
	if row, _ := spool.Row(reply); row.State != adaptercloud.SpoolReady || len(row.Sealed) == 0 {
		t.Fatalf("the terminal reply was not kept: %+v", row)
	}
	burned := spool.TakeBurned()
	if len(burned) != 3 {
		t.Fatalf("the waiters of %d retired rows are told, want 3", len(burned))
	}
	// A late receipt for the retired sent row is a receipt, not a revival.
	if result, err := spool.Settle(sent, "t/mac-01/sent", adaptercloud.SettleDelivered); err != nil ||
		result != adaptercloud.SettleResultLateIgnored {
		t.Fatalf("a late ack: %v %v", result, err)
	}
	if d := spool.SendNext(); d.Row == nil || d.Row.Seq != reply {
		t.Fatalf("after the cutover the next write is %+v, want the terminal reply %d", d, reply)
	}
}

// A halted epoch publishes nothing and reserves nothing: a sequence spent on
// an answer that cannot be sealed under a known key would hold every later
// row behind it.
func TestAHaltedEpochPublishesNothing(t *testing.T) {
	keys, err := domaincloud.NewKeyEpoch(epochState(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	relay, spool, _ := epochRelay(t, keys)
	if _, err := keys.Cutover(func(domaincloud.ContentKeyState) (domaincloud.ContentKeyState, error) {
		return domaincloud.ContentKeyState{}, fmt.Errorf("disk full")
	}, spool.RetireForCutover); err == nil {
		t.Fatal("a failed rotation reported success")
	}
	before := spool.NextSequence()
	if err := relay.Publish(context.Background(), Outbound{Channel: "t/mac-01/s", Payload: []byte(`{}`)}); !errors.Is(err, domaincloud.ErrKeyEpochHalted) {
		t.Fatalf("a halted epoch published: %v", err)
	}
	if spool.NextSequence() != before {
		t.Fatal("a halted epoch spent a sequence")
	}
}
