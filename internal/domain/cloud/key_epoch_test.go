package cloud

import (
	"crypto/rand"
	"errors"
	"sync"
	"testing"
)

func testEpochState(t *testing.T, epoch uint64) ContentKeyState {
	t.Helper()
	key, err := NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := rekeyKeyID(epoch, key.Bytes())
	return ContentKeyState{Epoch: epoch, KeyID: id, Key: key}
}

// Whoever reads the state inside Use while cutovers run sees a key and the id
// that names it, never one epoch's key with another's id.
func TestKeyEpochUseNeverTearsAKeyFromItsID(t *testing.T) {
	first := testEpochState(t, 1)
	e, err := NewKeyEpoch(first)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	byID := map[string][]byte{first.KeyID: first.Key.Bytes()}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for epoch := uint64(2); epoch < 200; epoch++ {
			next := testEpochState(t, epoch)
			mu.Lock()
			byID[next.KeyID] = next.Key.Bytes()
			mu.Unlock()
			if _, err := e.Cutover(func(ContentKeyState) (ContentKeyState, error) { return next, nil },
				func(_, _ ContentKeyState) error { return nil }); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 2000 {
				_ = e.Use(func(state ContentKeyState) error {
					mu.Lock()
					want := byID[state.KeyID]
					mu.Unlock()
					if string(want) != string(state.Key.Bytes()) {
						t.Errorf("%s carries another epoch's key", state.KeyID)
					}
					return nil
				})
			}
		}()
	}
	wg.Wait()
}

// Each step of a cutover that fails halts the epoch with the old state kept,
// and only Recover reopens it; a rotation that answers an epoch that is not
// later is refused as a regression.
func TestKeyEpochFailsClosed(t *testing.T) {
	ok := func(_, _ ContentKeyState) error { return nil }
	boom := errors.New("boom")
	cases := map[string]struct {
		rotate func(ContentKeyState) (ContentKeyState, error)
		retire KeyRetire
		want   error
	}{
		"rotate fails":     {func(ContentKeyState) (ContentKeyState, error) { return ContentKeyState{}, boom }, ok, boom},
		"retire fails":     {func(ContentKeyState) (ContentKeyState, error) { return testEpochState(t, 2), nil }, func(_, _ ContentKeyState) error { return boom }, boom},
		"same epoch":       {func(s ContentKeyState) (ContentKeyState, error) { return s, nil }, ok, ErrKeyEpochRegress},
		"invalid state":    {func(ContentKeyState) (ContentKeyState, error) { return ContentKeyState{Epoch: 2}, nil }, ok, ErrContentKeyState},
		"a reply key's id": {func(s ContentKeyState) (ContentKeyState, error) { s.Epoch, s.KeyID = 2, "rk-x"; return s, nil }, ok, ErrContentKeyState},
	}
	for name, c := range cases {
		first := testEpochState(t, 1)
		e, err := NewKeyEpoch(first)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.Cutover(c.rotate, c.retire); !errors.Is(err, ErrKeyEpochHalted) || !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
		ran := false
		if err := e.Use(func(ContentKeyState) error { ran = true; return nil }); !errors.Is(err, ErrKeyEpochHalted) || ran {
			t.Errorf("%s: Use on a halted epoch ran=%v err=%v", name, ran, err)
		}
		if _, err := e.Current(); !errors.Is(err, ErrKeyEpochHalted) {
			t.Errorf("%s: Current on a halted epoch: %v", name, err)
		}
		if got, err := e.Recover(func() (ContentKeyState, error) { return first, nil }, ok); err != nil || got.KeyID != first.KeyID {
			t.Errorf("%s: recovering the unchanged epoch: %v", name, err)
		}
		if e.Halted() != nil {
			t.Errorf("%s: still halted after recovery", name)
		}
	}
}

// Recovery adopts a later epoch only after retiring the old one's frames, and
// a retire that fails leaves the epoch halted at the old state, so a second
// recovery retries the retire rather than skipping it.
func TestKeyEpochRecoveryRetiresBeforeInstalling(t *testing.T) {
	first := testEpochState(t, 1)
	e, _ := NewKeyEpoch(first)
	next := testEpochState(t, 2)
	retired := 0
	failing := func(_, _ ContentKeyState) error { retired++; return errors.New("spool refused") }
	if _, err := e.Recover(func() (ContentKeyState, error) { return next, nil }, failing); !errors.Is(err, ErrKeyEpochHalted) {
		t.Fatalf("a failed retire: %v", err)
	}
	if _, err := e.Recover(func() (ContentKeyState, error) { return next, nil },
		func(old, n ContentKeyState) error {
			if old.KeyID != first.KeyID || n.KeyID != next.KeyID {
				t.Fatalf("retire %s -> %s", old.KeyID, n.KeyID)
			}
			retired++
			return nil
		}); err != nil {
		t.Fatal(err)
	}
	if state, _ := e.Current(); state.KeyID != next.KeyID || retired != 2 {
		t.Fatalf("installed %s after %d retires", state.KeyID, retired)
	}
	sameEpochOtherKey := testEpochState(t, 2)
	if _, err := e.Recover(func() (ContentKeyState, error) { return sameEpochOtherKey, nil },
		func(_, _ ContentKeyState) error { return nil }); !errors.Is(err, ErrKeyEpochRegress) {
		t.Fatalf("another key at the same epoch: %v", err)
	}
}

func TestContentKeyStateSendable(t *testing.T) {
	state := ContentKeyState{Epoch: 2, KeyID: "mk-2-x"}
	for keyID, want := range map[string]bool{"mk-2-x": true, "rk-abc": true, "ms-1": false, "": false} {
		if state.Sendable(keyID) != want {
			t.Errorf("Sendable(%q) = %v", keyID, !want)
		}
	}
}

// A malformed trusted callback must not keep rows labelled with a retired ID.
func TestKeyEpochRejectsAnIDFromAnotherKeyOrEpoch(t *testing.T) {
	key, err := NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first := ContentKeyState{Epoch: 2, KeyID: rekeyKeyID(2, key.Bytes()), Key: key}
	for _, kind := range []string{"wrong hash", "reused ID"} {
		t.Run(kind, func(t *testing.T) {
			next := first
			if kind == "wrong hash" {
				next.KeyID = "mk-2-wrong"
			} else {
				next.Epoch = 3
			}
			if _, err := NewKeyEpoch(next); !errors.Is(err, ErrContentKeyState) {
				t.Errorf("constructor accepted mismatched state: %v", err)
			}
			for _, recovery := range []bool{false, true} {
				e, err := NewKeyEpoch(first)
				if err != nil {
					t.Fatal(err)
				}
				retired := false
				retire := func(_, _ ContentKeyState) error { retired = true; return nil }
				if recovery {
					_, err = e.Recover(func() (ContentKeyState, error) { return next, nil }, retire)
				} else {
					_, err = e.Cutover(func(ContentKeyState) (ContentKeyState, error) { return next, nil }, retire)
				}
				if !errors.Is(err, ErrContentKeyState) || !errors.Is(err, ErrKeyEpochHalted) || retired {
					t.Errorf("recovery=%v: err=%v retired=%v", recovery, err, retired)
				}
				if _, err := e.Current(); !errors.Is(err, ErrKeyEpochHalted) {
					t.Errorf("invalid callback did not halt: %v", err)
				}
			}
		})
	}
}
