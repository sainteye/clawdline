package transcript

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
)

const syntheticInjection = "Synthetic immutable context.\nRole ALDER; language zh-Hant-TW; memory MAPLE."

func contextFixture(t *testing.T) (string, string, ports.AntigravityBinding, AntigravityContextCheck, string) {
	t.Helper()
	home, path := antigravityTestPath(t)
	bytes, err := os.ReadFile("testdata/antigravity-context.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	b := ports.AntigravityBinding{Process: ports.AntigravityProcess{PID: 42, ParentPID: 41, Birth: "2026-10-01T12:00:00Z", Executable: "/synthetic/agy", KernelGeneration: "synthetic-exec", BootID: "synthetic-boot", TerminalID: "%42", TTY: "ttysynthetic"}, ConversationID: syntheticAntigravityID, TranscriptPath: path, Event: "PreInvocation", Sequence: 1, Verified: true, ObservedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	check := func(_ context.Context, got ports.AntigravityBinding) error {
		if got != b {
			return errors.New("synthetic native binding changed")
		}
		return nil
	}
	return home, path, b, check, string(bytes)
}

func TestAntigravityContextNativeFixtureAndCurrentProof(t *testing.T) {
	home, path, b, check, row := contextFixture(t)
	ctx := context.Background()
	// This wrapper comes from a saved native row with only context/identity
	// sanitized; the test does not build it with the implementation's constants.
	var native map[string]any
	if json.Unmarshal([]byte(row), &native) != nil || native["source"] != "SYSTEM_SDK" || native["type"] != "SYSTEM_MESSAGE" || native["status"] != "DONE" {
		t.Fatal("native fixture attribution lost")
	}
	initial := antigravityTestUser(0, "load the synthetic context")
	writeAntigravityTest(t, path, initial)
	fence, err := CaptureAntigravityContextFence(ctx, home, b, check)
	if err != nil {
		t.Fatal(err)
	}
	if fence.Bytes != int64(len(initial)) || fence.LastStep != 0 || fence.Binding != b {
		t.Fatal("wrong baseline", fence)
	}
	writeAntigravityTest(t, path, initial+row)
	proof, err := VerifyAntigravityContextLoaded(ctx, home, fence, syntheticInjection, check)
	if err != nil {
		t.Fatal(err)
	}
	if proof.RowStart != int64(len(initial)) || proof.RowEnd != int64(len(initial+row)) || proof.StepIndex != 1 || proof.InjectionDigest != contextDigest([]byte(syntheticInjection)) {
		t.Fatal("wrong native row proof", proof)
	}
	writeAntigravityTest(t, path, initial+row+antigravityTestPlanner(2, "accepted", 1, 1, 0))
	if err := RecheckAntigravityContextLoaded(ctx, home, proof, syntheticInjection, check); err != nil {
		t.Fatal("ordinary append invalidated proof", err)
	}
	// Current validation also fences the proof row, not only the old baseline.
	writeAntigravityTest(t, path, initial+strings.Replace(row, "12:00:01Z", "12:00:02Z", 1))
	if err := RecheckAntigravityContextLoaded(ctx, home, proof, syntheticInjection, check); !errors.Is(err, ErrCursorStale) {
		t.Fatal("proof row rewrite accepted", err)
	}
}

func TestAntigravityContextRequiresNewUnambiguousNativeSystemRow(t *testing.T) {
	for _, kind := range []string{"model", "user", "generic-system", "error-status", "error", "null-error", "duplicate-source", "duplicate-content", "duplicate-error", "wrong-wrapper", "trimmed", "escaped-twice", "wrong-text", "old-offset", "wrong-step", "wrong-boundary"} {
		t.Run(kind, func(t *testing.T) {
			home, path, b, check, row := contextFixture(t)
			initial := antigravityTestUser(0, "synthetic request")
			writeAntigravityTest(t, path, initial)
			fence, err := CaptureAntigravityContextFence(context.Background(), home, b, check)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal([]byte(row), &object); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "model":
				object["source"] = "MODEL"
			case "user":
				object["source"] = "USER_EXPLICIT"
			case "generic-system":
				object["source"] = "SYSTEM"
			case "error-status":
				object["status"] = "ERROR"
			case "error":
				object["error"] = "synthetic failure"
			case "null-error":
				object["error"] = nil
			case "wrong-wrapper":
				object["content"] = syntheticInjection
			case "trimmed":
				object["content"] = strings.TrimSpace(object["content"].(string)) + " "
			case "escaped-twice":
				encoded, _ := json.Marshal(object["content"])
				object["content"] = string(encoded)
			case "wrong-text":
				object["content"] = strings.Replace(object["content"].(string), "MAPLE", "CEDAR", 1)
			case "old-offset":
				initial += row
				writeAntigravityTest(t, path, initial)
				fence, err = CaptureAntigravityContextFence(context.Background(), home, b, check)
				if err != nil {
					t.Fatal(err)
				}
				row = ""
			case "wrong-step":
				fence.LastStep++
			case "wrong-boundary":
				fence.Bytes--
				fence.PrefixDigest = contextDigest([]byte(initial[:len(initial)-1]))
			}
			if kind != "old-offset" {
				data, _ := json.Marshal(object)
				row = string(data) + "\n"
				switch kind {
				case "duplicate-source":
					row = `{"source":"MODEL",` + row[1:]
				case "duplicate-content":
					row = `{"content":"synthetic decoy",` + row[1:]
				case "duplicate-error":
					row = `{"error":"synthetic failure","error":"",` + row[1:]
				}
			}
			writeAntigravityTest(t, path, initial+row)
			if proof, err := VerifyAntigravityContextLoaded(context.Background(), home, fence, syntheticInjection, check); err == nil {
				t.Fatal("false native proof", proof)
			}
		})
	}
}

func TestAntigravityContextMissingReplacedAndChangedPrefix(t *testing.T) {
	for _, kind := range []string{"missing", "partial", "rewrite", "late-rewrite", "truncate", "replace", "symlink", "wrong-path", "unknown-generation", "empty-context", "missing-check", "cancel", "revoked-after-read"} {
		t.Run(kind, func(t *testing.T) {
			home, path, b, check, row := contextFixture(t)
			initial := antigravityTestUser(0, strings.Repeat("synthetic prefix ", 400))
			writeAntigravityTest(t, path, initial)
			fence, err := CaptureAntigravityContextFence(context.Background(), home, b, check)
			if err != nil {
				t.Fatal(err)
			}
			writeAntigravityTest(t, path, initial+row)
			ctx := context.Background()
			expected := syntheticInjection
			switch kind {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "partial":
				writeAntigravityTest(t, path, initial+row+"{")
			case "rewrite":
				writeAntigravityTest(t, path, strings.Replace(initial, "prefix", "PREFIX", 1)+row)
			case "late-rewrite":
				data := []byte(initial + row)
				data[5000] = 'X'
				writeAntigravityTest(t, path, string(data))
			case "truncate":
				writeAntigravityTest(t, path, row)
			case "replace":
				writeAntigravityTest(t, path+".new", initial+row)
				if err := os.Rename(path+".new", path); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(t.TempDir(), "outside")
				writeAntigravityTest(t, target, initial+row)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "wrong-path":
				fence.Binding.TranscriptPath = path + ".other"
			case "unknown-generation":
				fence.Generation = "unknown"
			case "empty-context":
				expected = ""
			case "missing-check":
				check = nil
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "revoked-after-read":
				calls := 0
				check = func(context.Context, ports.AntigravityBinding) error {
					calls++
					if calls > 1 {
						return errors.New("synthetic revocation")
					}
					return nil
				}
			}
			if proof, err := VerifyAntigravityContextLoaded(ctx, home, fence, expected, check); err == nil {
				t.Fatal("unsafe file became proof", proof)
			}
		})
	}
}

func TestAntigravityContextCaptureNeverManufacturesMissingBaseline(t *testing.T) {
	home, path, b, check, _ := contextFixture(t)
	if _, err := CaptureAntigravityContextFence(context.Background(), home, b, check); err == nil {
		t.Fatal("missing file became offset zero")
	}
	writeAntigravityTest(t, path, "{")
	if _, err := CaptureAntigravityContextFence(context.Background(), home, b, check); err == nil {
		t.Fatal("partial file became baseline")
	}
	writeAntigravityTest(t, path, "")
	fence, err := CaptureAntigravityContextFence(context.Background(), home, b, check)
	if err != nil || fence.Bytes != 0 || fence.LastStep != -1 || fence.Generation == "unknown" {
		t.Fatal("existing empty file lost its generation", fence, err)
	}
	calls := 0
	check = func(context.Context, ports.AntigravityBinding) error {
		calls++
		if calls > 1 {
			return errors.New("synthetic generation changed")
		}
		return nil
	}
	if _, err := CaptureAntigravityContextFence(context.Background(), home, b, check); err == nil {
		t.Fatal("revocation after capture ignored")
	}
}

func TestAntigravityContextFirstNativeTurnKeepsTrailingNewline(t *testing.T) {
	home, path, binding, check, _ := contextFixture(t)
	row, err := os.ReadFile("testdata/antigravity-first-input-context.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// The wrapper and context bytes come from one native first-turn probe;
	// only its timestamp was sanitized. The context ends with a newline.
	const expected = "Synthetic immutable protocol context. Role marker ROLE_COPPER; language marker LANG_TAIWAN; memory marker MEMORY_FIR. Do not use tools, network, search, browser, connectors or paid credits. Reply only to the synthetic prompt.\n"
	initial := antigravityTestUser(0, "synthetic first input")
	writeAntigravityTest(t, path, initial)
	fence, err := CaptureAntigravityContextFence(context.Background(), home, binding, check)
	if err != nil {
		t.Fatal(err)
	}
	writeAntigravityTest(t, path, initial+string(row))
	proof, err := VerifyAntigravityContextLoaded(context.Background(), home, fence, expected, check)
	if err != nil || proof.RowStart != int64(len(initial)) || proof.StepIndex != 1 {
		t.Fatal("first native system row did not verify", err)
	}
	if _, err := VerifyAntigravityContextLoaded(context.Background(), home, fence, strings.TrimSuffix(expected, "\n"), check); err == nil {
		t.Fatal("trimmed context was accepted")
	}
}
