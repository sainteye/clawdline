package http

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// Opt-in private replay; no provider conversation is a repository fixture.
// The input holds fixed API pages and the native items matched to their rows.
func TestReplayToolResultTraffic(t *testing.T) {
	path := os.Getenv("CLAWDLINE_TOOL_RESULT_REPLAY")
	if path == "" {
		t.Skip("set CLAWDLINE_TOOL_RESULT_REPLAY to a private replay export")
	}
	var input []struct {
		Page  contract.TranscriptPage `json:"page"`
		Items []struct {
			At   int64          `json:"at"`
			Item map[string]any `json:"item"`
		} `json:"items"`
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private replay export unreadable")
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal("invalid private replay export")
	}
	signer, err := domaincloud.NewDeviceKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := domaincloud.NewContentKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wire := func(page any, view string) int {
		b := cloudops.Bridge{MachineID: "machine-replay", TranscriptAuthority: func(context.Context, string, ed25519.PublicKey) cloudops.Authority {
			return cloudops.Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true, ReadTranscriptAllows: true, WriteGateAllows: true}
		}, AdmitExecution: func(context.Context, string, string, string) error { return nil }, Router: cloudopsRouter(func(cloudops.LocalRequest) cloudops.LocalResponse {
			body, _ := json.Marshal(page)
			return cloudops.LocalResponse{Status: 200, ContentType: "application/json", Body: body}
		})}
		fields := map[string]any{"type": "transcript", "machine_id": "machine-replay", "session": "synthetic-session", "limit": 200, "expected_generation": "0123456789abcdef0123456789abcdef"}
		if view != "" {
			fields["view"] = view
		}
		plain, _ := json.Marshal(fields)
		answer := b.Handle(context.Background(), cloudops.Command{Channel: "r/machine-replay", Class: cloudops.ClassCtl, Sequence: 1, Plaintext: plain})
		if !answer.OK() {
			t.Fatalf("replay bridge refused: %s", answer.Code)
		}
		envelope, err := domaincloud.Seal(answer.Payload, domaincloud.SealParams{Ch: "t/machine-replay/synthetic-session", Seq: 1, Ts: 1793000000, Class: domaincloud.ClassStream, KeyID: "ms-1", Sender: "machine-replay", Key: key, Signer: signer})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(envelope)
		return len(encoded)
	}
	var rows, calls, long, oldBytes, newBytes, textBytes int
	output := struct {
		Pages   []contract.TranscriptPage `json:"pages"`
		Details map[string]any            `json:"details"`
	}{Details: map[string]any{}}
	for n, source := range input {
		record := filepath.Join(t.TempDir(), "record.jsonl")
		f, err := os.Create(record)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range source.Items {
			_ = json.NewEncoder(f).Encode(map[string]any{"type": "event_msg", "timestamp": time.Unix(row.At, 0).UTC().Format("2006-01-02T15:04:05.000Z07:00"), "payload": map[string]any{"type": "item_completed", "item": row.Item}})
		}
		f.Close()
		parsed, err := transcript.ReadCodexBefore(record, len(source.Items)+1, 0)
		if err != nil {
			t.Fatal(err)
		}
		byKey := map[string]transcript.Entry{}
		for _, e := range parsed.Entries {
			if e.Activity != nil && e.Activity.Title != nil {
				byKey[strconv.FormatInt(e.At, 10)+"\x00"+*e.Activity.Title] = e
			}
		}
		page := source.Page
		oldBytes += wire(page, "")
		page.Entries = append([]contract.TranscriptEntry{}, page.Entries...)
		rows += len(page.Entries)
		for i, row := range page.Entries {
			if row.Activity == nil || row.Activity.Kind != "called" {
				continue
			}
			e, ok := byKey[strconv.FormatInt(row.At, 10)+"\x00"+row.Activity.Title]
			if !ok {
				t.Fatalf("fixed row missing: page=%d row=%d at=%d parsed=%d expected=%d", n, i, row.At, len(parsed.Entries), len(source.Items))
			}
			a := *row.Activity
			row.Activity = &a
			page.Entries[i] = compactToolEntry(row, e, "synthetic-scope-"+strconv.Itoa(n))
			calls++
			result := e.Activity.ToolResult
			ref := page.Entries[i].Activity.ToolResult.Reference
			for _, representation := range []string{"text", "raw"} {
				value := result.Text()
				if representation == "raw" {
					value = result.Raw()
				}
				clipped := transcript.ClipUTF8(value, transcriptBudget)
				output.Details[ref+":"+representation] = map[string]any{"reference": ref, "representation": representation, "text": clipped, "bytes": len(value), "truncated": len(clipped) < len(value), "sourceTruncated": result.SourceTruncated}
			}
			if len(result.Text()) > len(result.Preview) {
				long++
				value := transcript.ClipUTF8(result.Text(), transcriptBudget)
				ref := page.Entries[i].Activity.ToolResult.Reference
				textBytes += wire(map[string]any{"reference": ref, "representation": "text", "text": value, "bytes": len(result.Text()), "truncated": len(value) < len(result.Text()), "sourceTruncated": result.SourceTruncated}, "tool-v1z:"+ref+":text")
			}
		}
		page.Version = transcriptVersion(page, "synthetic-scope-"+strconv.Itoa(n))
		newBytes += wire(page, "tools-v1z")
		output.Pages = append(output.Pages, page)
	}
	t.Logf("fixed_rows=%d called=%d long=%d old_envelopes=%d compact_gzip_envelopes=%d all_long_text_envelopes=%d cold_saving=%.2f%% twenty_percent_long_saving=%.2f%%", rows, calls, long, oldBytes, newBytes, textBytes, 100*(1-float64(newBytes)/float64(oldBytes)), 100*(1-(float64(newBytes)+0.2*float64(textBytes))/float64(oldBytes)))
	if target := os.Getenv("CLAWDLINE_TOOL_RESULT_REPLAY_OUTPUT"); target != "" {
		encoded, _ := json.Marshal(output)
		if err := os.WriteFile(target, encoded, 0600); err != nil {
			t.Fatal("private replay output could not be written")
		}
	}
	if calls == 0 || rows == 0 || newBytes >= oldBytes {
		t.Fatal("replay did not reduce the fixed reply bytes")
	}
}

type cloudopsRouter func(cloudops.LocalRequest) cloudops.LocalResponse

func (f cloudopsRouter) Do(_ context.Context, r cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	return f(r), nil
}
