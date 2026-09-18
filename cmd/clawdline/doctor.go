package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/sainteye/clawdline-go/internal/adapters/devices"
	"github.com/sainteye/clawdline-go/internal/adapters/logs"
	"github.com/sainteye/clawdline-go/internal/adapters/push"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// A drill fills one row of the capacity register on purpose, through the
// row's own write path, in a throwaway directory (docs/limits.md §4.7 layer 3).
// Each row it can fill is here, with what its at-limit behaviour is expected
// to move.
type drill struct {
	// action is the counter the row's at-limit behaviour moves: rotated,
	// refused or evicted.
	action string
	// refusal is the typed error a refused write must be, for a row that
	// refuses.
	refusal error
	// limit is the limit the drill runs the row at by default.
	limit string
	// open prepares the row's writer in dir at limit. write adds one thing
	// (the nth); read is the row's reading.
	open func(dir string, limit int64) (write func(n int) error, read func() capacity.Reading, err error)
}

var drills = map[string]drill{
	capacity.AuditSecurity: {action: "rotated", limit: "4KiB", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		files, err := devices.Open(dir)
		if err != nil {
			return nil, nil, err
		}
		files.SetAuditLimit(limit)
		return func(n int) error {
			files.Audit("capacity.drill", map[string]string{"n": strconv.Itoa(n)})
			return nil
		}, files.AuditReading, nil
	}},
	capacity.LogDaemon: {action: "rotated", limit: "4KiB", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		w, err := logs.Open(dir)
		if err != nil {
			return nil, nil, err
		}
		w.SetLimit(limit)
		w.SetFallback(io.Discard)
		return func(n int) error {
			_, err := fmt.Fprintf(w, "capacity drill: line %d of the daemon's log\n", n)
			return err
		}, w.Reading, nil
	}},
	capacity.DevicesList: {action: "refused", refusal: devices.ErrDeviceListFull, limit: "20", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		files, err := devices.Open(dir)
		if err != nil {
			return nil, nil, err
		}
		files.SetDeviceLimit(limit)
		// The authority is what pairing, a password sign-in and
		// `clawdline open` add a device through.
		a, err := auth.New(files, auth.Options{})
		if err != nil {
			return nil, nil, err
		}
		return func(n int) error {
			_, _, err := a.AddDevice("capacity drill "+strconv.Itoa(n), auth.NewCaps(auth.Read), false)
			return err
		}, files.DevicesReading, nil
	}},
	capacity.PushSubscriptions: {action: "refused", refusal: push.ErrSubscriptionsFull, limit: "20", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		store, err := push.Open(dir)
		if err != nil {
			return nil, nil, err
		}
		store.SetLimit(limit)
		key := append([]byte{0x04}, make([]byte, push.SubscriberKeyBytes-1)...)
		return func(n int) error {
			// What the subscribe route is handed, through the same check.
			sub, ok := push.FromBrowser(map[string]any{
				"endpoint": fmt.Sprintf("https://push.invalid/capacity-drill/%d", n),
				"keys": map[string]any{
					"p256dh": base64.RawURLEncoding.EncodeToString(key),
					"auth":   base64.RawURLEncoding.EncodeToString(make([]byte, push.AuthSecretBytes)),
				},
			}, fmt.Sprintf("drill%d", n), fmt.Sprintf("drill-device-%d", n), "")
			if !ok {
				return errors.New("the drill's subscription was not accepted by FromBrowser")
			}
			return store.Add(sub)
		}, store.Reading, nil
	}},
	capacity.CacheTranscriptUsage: {action: "evicted", limit: "20", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		l := transcript.NewLedger()
		l.SetLimit(limit)
		return func(n int) error {
			path, err := drillTranscript(dir, n)
			if err != nil {
				return err
			}
			_, err = l.Claude(path)
			return err
		}, l.Reading, nil
	}},
	capacity.CacheTranscriptTitles: {action: "evicted", limit: "20", open: func(dir string, limit int64) (func(int) error, func() capacity.Reading, error) {
		t := transcript.NewTitles()
		t.SetLimit(limit)
		return func(n int) error {
			path, err := drillTranscript(dir, n)
			if err != nil {
				return err
			}
			t.Read(path)
			return nil
		}, t.Reading, nil
	}},
}

// drillTranscript writes the nth one-line transcript the cache drills read.
func drillTranscript(dir string, n int) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("drill-%04d.jsonl", n))
	line := fmt.Sprintf(`{"type":"assistant","aiTitle":"drill %d","message":{"model":"drill","usage":{"input_tokens":1,"output_tokens":1}}}`+"\n", n)
	return path, os.WriteFile(path, []byte(line), 0o600)
}

// drillNames is the rows with a drill, for the usage line.
func drillNames() []string {
	out := make([]string, 0, len(drills))
	for name := range drills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// counterOf is the reading's counter named by a drill's action.
func counterOf(r capacity.Reading, action string) int64 {
	switch action {
	case "rotated":
		return r.Counters.Rotated
	case "refused":
		return r.Counters.Refused
	case "evicted":
		return r.Counters.Evicted
	}
	return 0
}
