package cloud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/healthsealed"
	"github.com/sainteye/clawdline/internal/domain/cloud"
)

func TestContentEpochIsUnknownUntilRecordedForThisSecret(t *testing.T) {
	s := ContentEpochStore{Dir: t.TempDir()}
	master := make([]byte, 32)
	rand.Read(master)
	if _, ok, err := s.Load("usr_a", master); ok || err != nil {
		t.Fatalf("no record must be unknown, got ok=%v err=%v", ok, err)
	}
	id, _ := cloud.ContentKeyID(master)
	if err := s.Save(ContentEpoch{V: 1, ContentKeyEpoch: 1, KeyID: id, AccountID: "usr_a"}); err != nil {
		t.Fatal(err)
	}
	if e, ok, _ := s.Load("usr_a", master); !ok || e != 1 {
		t.Fatalf("recorded epoch: %d %v", e, ok)
	}
	other := make([]byte, 32)
	rand.Read(other)
	if _, ok, _ := s.Load("usr_a", other); ok {
		t.Fatal("a replaced secret inherited the epoch")
	}
	if _, ok, _ := s.Load("usr_b", master); ok {
		t.Fatal("another account inherited the epoch")
	}
	if err := s.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load("usr_a", master); ok {
		t.Fatal("forgotten epoch still answers")
	}
}

// e2eConfig is written by the Node fixture that runs Cloud's real API with
// memory repositories (tools are in the task's artifacts). Every key in it is
// synthetic and generated for one run.
type e2eConfig struct {
	BaseURL     string `json:"base_url"`
	AccountID   string `json:"account_id"`
	MachineID   string `json:"machine_id"`
	Credential  string `json:"credential"`
	MachineSeed string `json:"machine_seed"`
	Dir         string `json:"dir"`
}

func waitFile(ctx context.Context, path string) ([]byte, error) {
	for {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return b, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// TestIdentityPairingEndToEnd is the machine in the local experiment: it runs
// identity pairing against Cloud's real routes, then seals and uploads one
// diagnostic with the epoch the grant recorded. Skipped unless the fixture
// set CLAWDLINE_IDENTITY_E2E to its config file.
func TestIdentityPairingEndToEnd(t *testing.T) {
	path := os.Getenv("CLAWDLINE_IDENTITY_E2E")
	if path == "" {
		t.Skip("set by the identity-pairing experiment fixture only")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg e2eConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	seed, _ := base64.StdEncoding.DecodeString(cfg.MachineSeed)
	machineKey, err := cloud.NewDeviceKey(bytes.NewReader(seed))
	if err != nil {
		t.Fatal(err)
	}
	master := make([]byte, 32)
	rand.Read(master)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := &IdentityClient{BaseURL: cfg.BaseURL, Credential: func(context.Context) (string, error) { return cfg.Credential, nil }}
	epochs := ContentEpochStore{Dir: filepath.Join(cfg.Dir, "machine")}
	masterFn := func() ([]byte, bool, error) { return append([]byte(nil), master...), true, nil }

	// A wrong code on this machine hands over nothing (first pairing,
	// person refuses); the fixture's viewer sees the pairing never grant.
	for _, round := range []string{"refuse", "pair"} {
		run := &MachineIdentityPairing{
			Client: client, MachineKey: machineKey, Master: masterFn, Epochs: epochs,
			Show: func(fragment string) {
				os.WriteFile(filepath.Join(cfg.Dir, round+".qr"), []byte(fragment), 0o600)
			},
			Approve: func(ctx context.Context, fingerprint, code string) (bool, error) {
				seen, err := waitFile(ctx, filepath.Join(cfg.Dir, round+".code"))
				if err != nil {
					return false, err
				}
				return round == "pair" && strings.TrimSpace(string(seen)) == code, nil
			},
		}
		res, err := run.Run(ctx, cfg.AccountID)
		if round == "refuse" {
			if !errors.Is(err, cloud.ErrPairingNotApproved) {
				t.Fatalf("refused code: got %v", err)
			}
			if _, ok, _ := epochs.Load(cfg.AccountID, master); ok {
				t.Fatal("a refused pairing recorded an epoch")
			}
			continue
		}
		if err != nil {
			t.Fatalf("pairing: %v", err)
		}
		t.Logf("paired viewer %s epoch %d", res.ViewerDeviceID, res.ContentKeyEpoch)
	}

	up := &healthsealed.Uploader{
		Endpoint: cfg.BaseURL,
		Enabled:  func() bool { return true },
		Keys: healthsealed.ProviderSource{
			Identity: func(context.Context) (string, string, string, int64, error) {
				return cfg.AccountID, cfg.MachineID, cfg.Credential, 0, nil
			},
			Master: masterFn,
			ContentEpoch: func() (int64, bool) {
				e, ok, err := epochs.Load(cfg.AccountID, master)
				return e, ok && err == nil
			},
		},
	}
	store, err := healthsealed.OpenStore(filepath.Join(cfg.Dir, "sealed"))
	if err != nil {
		t.Fatal(err)
	}
	up.Store = store
	snap := healthsealed.Snapshot{Schema: healthsealed.SnapshotSchema, Location: "e2e.go:1", Stage: "relay_write",
		AgeNS: healthsealed.Int64{Value: 1_500_000_000, Set: true}, ObservedAtMS: healthsealed.Int64{Value: time.Now().UnixMilli(), Set: true}}
	pending, err := up.Seal(ctx, "boot-e2e", "evidence", "ev-e2e", snap)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	receipt, err := up.Send(ctx, pending.AttachmentID)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	out, _ := json.Marshal(map[string]any{"attachment_id": pending.AttachmentID, "revision": pending.Revision,
		"content_key_epoch": receipt.ContentKeyEpoch, "machine_id": cfg.MachineID})
	if err := os.WriteFile(filepath.Join(cfg.Dir, "uploaded.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeEpochBridgeNeverOverwritesAnInconsistentFloor(t *testing.T) {
	key, err := cloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := cloud.ContentKeyID(key.Bytes())
	state := cloud.ContentKeyState{Epoch: 3, KeyID: "not-used-by-record-check", Key: key}
	for _, tc := range []struct {
		name   string
		rec    ContentEpoch
		refuse bool
	}{
		{"historical", ContentEpoch{V: 1, AccountID: "account", ContentKeyEpoch: 1, KeyID: "old-key"}, false},
		{"current", ContentEpoch{V: 1, AccountID: "account", ContentKeyEpoch: 3, KeyID: id}, false},
		{"future", ContentEpoch{V: 1, AccountID: "account", ContentKeyEpoch: 4, KeyID: id}, true},
		{"same-epoch-other-key", ContentEpoch{V: 1, AccountID: "account", ContentKeyEpoch: 3, KeyID: "other"}, true},
		{"other-account", ContentEpoch{V: 1, AccountID: "other", ContentKeyEpoch: 1, KeyID: id}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := ContentEpochStore{Dir: t.TempDir()}
			if err := store.Save(tc.rec); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(store.Dir, ContentEpochFile))
			err := store.CheckRuntime("account", state)
			if (err != nil) != tc.refuse {
				t.Fatalf("floor result: %v", err)
			}
			after, _ := os.ReadFile(filepath.Join(store.Dir, ContentEpochFile))
			if !bytes.Equal(before, after) {
				t.Fatal("floor check overwrote journal")
			}
		})
	}
}
