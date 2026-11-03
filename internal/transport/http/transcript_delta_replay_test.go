package http

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// The explicit export stays private. Only aggregate byte counts leave this run.
func TestReplayTranscriptUpdateTraffic(t *testing.T) {
	directory := os.Getenv("CLAWDLINE_TRANSCRIPT_UPDATE_REPLAY_DIR")
	if directory == "" {
		t.Skip("explicit private paired export required")
	}
	paths, err := filepath.Glob(filepath.Join(directory, "page-a-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var seq uint64
	wire := func(raw []byte, read string) int {
		t.Helper()
		var proof map[string]any
		if err := json.Unmarshal(raw, &proof); err != nil {
			t.Fatal(err)
		}
		seq++
		proof["seq"] = seq
		proof["read"] = read
		proof["machine_id"] = "fixture-machine"
		proof["session_id"] = "fixture-session"
		proof["expected_generation"] = strings.Repeat("a", 32)
		payload, _ := json.Marshal(proof)
		var zipped bytes.Buffer
		zipped.Write([]byte{'g', 'z', '1', 0})
		w, _ := gzip.NewWriterLevel(&zipped, 6)
		w.Write(payload)
		w.Close()
		if zipped.Len() < len(payload) {
			payload = zipped.Bytes()
		}
		envelope, err := domaincloud.Seal(payload, domaincloud.SealParams{Ch: "t/fixture-machine/fixture-session", Seq: seq, Ts: 1, Class: domaincloud.ClassStream, KeyID: "ms-1", Sender: "fixture-signer", Key: key, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		frame, err := envelope.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return len(frame)
	}
	pairs, changed, patched, fullBytes, updateBytes, fullWire, updateWire := 0, 0, 0, 0, 0, 0, 0
	var elapsed time.Duration
	for _, path := range paths {
		if !regexp.MustCompile(`^page-a-\d+\.json$`).MatchString(filepath.Base(path)) {
			continue
		}
		read := func(path string) contract.TranscriptPage {
			t.Helper()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var p contract.TranscriptPage
			if json.Unmarshal(raw, &p) != nil || p.ID == "" || len(p.Version) != 64 {
				t.Fatal("invalid exported page")
			}
			return p
		}
		a, b := read(path), read(filepath.Join(directory, strings.Replace(filepath.Base(path), "page-a-", "page-b-", 1)))
		if a.ID != b.ID {
			t.Fatal("paired export changed Session")
		}
		pairs++
		var whole, answer any = b, b
		s := &Server{}
		s.transcriptDeltaAnswer(a, "", "fixed-pair", 200)
		start := time.Now()
		if a.Version == b.Version {
			whole = map[string]any{"unchanged": true, "version": a.Version}
			answer = whole
		} else {
			changed++
			answer = s.transcriptDeltaAnswer(b, a.Version, "fixed-pair", 200)
			if delta, ok := answer.(contract.TranscriptDelta); ok {
				patched++
				merged := applyTestDelta(t, a, delta)
				got, _ := json.Marshal(merged)
				want, _ := json.Marshal(b)
				if !bytes.Equal(got, want) {
					t.Fatal("replayed update differs from full captured page")
				}
			}
		}
		elapsed += time.Since(start)
		full, _ := json.Marshal(whole)
		update, _ := json.Marshal(answer)
		fullBytes += len(full)
		updateBytes += len(update)
		fullWire += wire(full, "transcript.view.tools-v2z:"+a.Version)
		updateWire += wire(update, "transcript.view.delta-v1z:"+a.Version)
	}
	if pairs == 0 {
		t.Fatal("no paired exports")
	}
	fmt.Printf("pairs=%d changed=%d patched=%d baseline_body=%d incremental_body=%d baseline_sealed=%d incremental_sealed=%d transform_total_us=%d body_reduction=%.4f sealed_reduction=%.4f\n", pairs, changed, patched, fullBytes, updateBytes, fullWire, updateWire, elapsed.Microseconds(), 1-float64(updateBytes)/float64(fullBytes), 1-float64(updateWire)/float64(fullWire))
}
