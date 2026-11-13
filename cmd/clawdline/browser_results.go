package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
)

const (
	maxBrowserCloseReceipts     = browserbridge.MaxPendingCommands
	maxBrowserCloseReceiptBytes = browserbridge.MaxResultBytes
)

// One native reader applies backpressure until its current result batch is
// acknowledged. Only minimal close receipts survive a host process restart;
// page text, input text, URLs and screenshots never enter this spool.
func browserCloseSpoolPath(dir, mode, profile string) string {
	sum := sha256.Sum256([]byte(mode + "\x00" + profile))
	return filepath.Join(dir, "browser-close-results-"+hex.EncodeToString(sum[:])+".json")
}

func browserCloseReceipts(raw json.RawMessage, closes map[string]bool) (json.RawMessage, error) {
	if len(raw) > maxBrowserCloseReceiptBytes {
		return nil, errors.New(cliCopy("core", "browser_host.results_too_large", "browser-host: result batch too large"))
	}
	var results []browserbridge.Result
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, err
	}
	var saved []browserbridge.Result
	for _, r := range results {
		if !closes[r.ID] {
			continue
		}
		delete(closes, r.ID)
		r.Value, r.URL, r.Target, r.Thumb = nil, "", "", ""
		if r.Error != "" && r.Error != "the tab was closed" {
			r.Error = "the browser could not close the tab"
		}
		saved = append(saved, r)
	}
	if len(saved) > maxBrowserCloseReceipts {
		return nil, errors.New(cliCopy("core", "browser_host.too_many_receipts", "browser-host: too many close receipts"))
	}
	if len(saved) == 0 {
		return nil, nil
	}
	return json.Marshal(saved)
}

func loadBrowserCloseResults(path string) (json.RawMessage, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxBrowserCloseReceiptBytes+1))
	if err != nil {
		return nil, err
	}
	var results []browserbridge.Result
	if len(raw) > maxBrowserCloseReceiptBytes || json.Unmarshal(raw, &results) != nil || len(results) > maxBrowserCloseReceipts {
		return nil, errors.New(cliCopy("core", "browser_host.invalid_spool", "browser-host: invalid close receipt spool"))
	}
	return raw, nil
}

func saveBrowserCloseResults(path string, raw json.RawMessage) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".browser-close-results-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func postBrowserHostResults(ctx context.Context, b *broker, results json.RawMessage, errOut io.Writer, modes ...string) bool {
	mode := ""
	if len(modes) > 0 {
		mode = modes[0]
	}
	for {
		a, err := b.requestContext(ctx, http.MethodPost, "/v1/browser/host/results", nil, struct {
			Mode    string          `json:"browser_mode,omitempty"`
			Results json.RawMessage `json:"results"`
		}{mode, results}, "")
		if err == nil && a.ok() {
			var ack struct {
				OK bool `json:"ok"`
			}
			if json.Unmarshal(a.Body, &ack) == nil && ack.OK {
				return true
			}
		}
		// Retry the same result, including after non-2xx responses. A lost ACK
		// is safe: Bridge.Deliver settles a command ID at most once.
		fmt.Fprintln(errOut, cliCopy("core", "browser_host.result_pending", "browser-host: result not acknowledged; retrying"))
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
}
