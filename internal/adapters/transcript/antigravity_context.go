package transcript

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// AntigravityContextFence is private evidence captured BEFORE exposing a
// PreInvocation response. It contains no injected text or capability. A caller
// must durably associate it with the exact prepared response, and allow only
// one outstanding response; an arbitrary historical fence is not authority.
type AntigravityContextFence struct {
	Binding      ports.AntigravityBinding `json:"binding"`
	Generation   string                   `json:"generation"`
	PrefixDigest string                   `json:"prefix_digest"`
	Bytes        int64                    `json:"bytes"`
	LastStep     int64                    `json:"last_step"`
}

// AntigravityContextProof proves that exact context occurred in this native
// execution after the fence. It does not prove which model invocation used it,
// tool permission enforcement, or that an instruction was followed. A current
// badge must recheck the live binding, prepared receipt and these file bytes.
type AntigravityContextProof struct {
	Fence           AntigravityContextFence `json:"fence"`
	RowStart        int64                   `json:"row_start"`
	RowEnd          int64                   `json:"row_end"`
	StepIndex       int64                   `json:"step_index"`
	PrefixDigest    string                  `json:"prefix_digest"`
	InjectionDigest string                  `json:"injection_digest"`
}

// AntigravityContextCheck is supplied by native composition. It must freshly
// verify the entire process generation, exact terminal, workspace, capability
// and the same positive conversation binding. Missing verification refuses.
type AntigravityContextCheck func(context.Context, ports.AntigravityBinding) error

func contextDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func checkAntigravityContext(ctx context.Context, home string, binding ports.AntigravityBinding, check AntigravityContextCheck) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, ok := AntigravityPath(home, binding.ConversationID)
	p := binding.Process
	if check == nil || !ok || binding.TranscriptPath != path || !binding.Verified || binding.Sequence == 0 || binding.Event != "PreInvocation" || p.PID <= 0 || p.ParentPID <= 0 || p.Birth == "" || p.Executable == "" || p.KernelGeneration == "" || p.TTY == "" || p.TerminalID == "" {
		return antigravityError("context_proof_unavailable")
	}
	if err := check(ctx, binding); err != nil {
		return err
	}
	return ctx.Err()
}

// CaptureAntigravityContextFence requires an existing, complete, confined
// native file with known generation. Missing file is never offset zero.
func CaptureAntigravityContextFence(ctx context.Context, home string, binding ports.AntigravityBinding, check AntigravityContextCheck) (AntigravityContextFence, error) {
	if err := checkAntigravityContext(ctx, home, binding, check); err != nil {
		return AntigravityContextFence{}, err
	}
	snapshot, err := readAntigravitySnapshot(binding.TranscriptPath)
	if err != nil {
		return AntigravityContextFence{}, err
	}
	if snapshot.generation == "unknown" {
		return AntigravityContextFence{}, antigravityError("context_proof_unavailable")
	}
	if err := checkAntigravityContext(ctx, home, binding, check); err != nil {
		return AntigravityContextFence{}, err
	}
	last := int64(-1)
	if len(snapshot.rows) > 0 {
		last = *snapshot.rows[len(snapshot.rows)-1].Index
	}
	return AntigravityContextFence{Binding: binding, Generation: snapshot.generation, PrefixDigest: contextDigest(snapshot.data), Bytes: int64(len(snapshot.data)), LastStep: last}, nil
}

// The exact wrapper was observed in saved standalone 1.4.5 native logs. No
// trimming, substring search, recursive unwrapping or model echo is accepted.
const antigravityContextPrefix = "The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n\n<SYSTEM_MESSAGE>\nThe following system message was injected by a PreInvocation hook:\n"
const antigravityContextSuffix = "\n</SYSTEM_MESSAGE>"

// VerifyAntigravityContextLoaded uses the existing whole-file ReadBudget,
// confinement and strict step ordering. It starts no poller or provider call.
func VerifyAntigravityContextLoaded(ctx context.Context, home string, fence AntigravityContextFence, expected string, check AntigravityContextCheck) (AntigravityContextProof, error) {
	empty := AntigravityContextProof{}
	if err := checkAntigravityContext(ctx, home, fence.Binding, check); err != nil {
		return empty, err
	}
	if expected == "" || fence.Bytes < 0 || fence.Bytes > ReadBudget || fence.LastStep < -1 || fence.Generation == "" || fence.Generation == "unknown" {
		return empty, antigravityError("context_proof_unavailable")
	}
	snapshot, err := readAntigravitySnapshot(fence.Binding.TranscriptPath)
	if err != nil {
		return empty, err
	}
	if snapshot.generation != fence.Generation || fence.Bytes > int64(len(snapshot.data)) || contextDigest(snapshot.data[:fence.Bytes]) != fence.PrefixDigest {
		return empty, ErrCursorStale
	}
	// Fence boundary and last step must agree with the validated full prefix.
	// A caller cannot cut through a row or skip an already present system row.
	last := int64(-1)
	for _, row := range snapshot.rows {
		if row.before >= fence.Bytes {
			break
		}
		last = *row.Index
	}
	if (fence.Bytes > 0 && snapshot.data[fence.Bytes-1] != '\n') || last != fence.LastStep {
		return empty, ErrCursorStale
	}
	want := antigravityContextPrefix + expected + antigravityContextSuffix
	for _, row := range snapshot.rows {
		if row.before < fence.Bytes || *row.Index <= fence.LastStep || row.Source != "SYSTEM_SDK" || row.Type != "SYSTEM_MESSAGE" || row.Status != "DONE" || row.Error != "" || row.Content != want {
			continue
		}
		rest := snapshot.data[row.before:]
		end := row.before + int64(bytes.IndexByte(rest, '\n')) + 1
		// Unmarshal alone silently accepts duplicate source/content/error keys.
		// This proof needs one unambiguous native attribution for the entire row.
		if !unambiguousAntigravityContextRow(snapshot.data[row.before:end]) {
			return empty, antigravityError("context_proof_ambiguous")
		}
		if err := checkAntigravityContext(ctx, home, fence.Binding, check); err != nil {
			return empty, err
		}
		return AntigravityContextProof{Fence: fence, RowStart: row.before, RowEnd: end, StepIndex: *row.Index, PrefixDigest: contextDigest(snapshot.data[:end]), InjectionDigest: contextDigest([]byte(expected))}, nil
	}
	if err := checkAntigravityContext(ctx, home, fence.Binding, check); err != nil {
		return empty, err
	}
	return empty, antigravityError("context_proof_unavailable")
}

// RecheckAntigravityContextLoaded invalidates a current badge after replacement,
// truncation or same-length edits, including edits to the proof row itself.
func RecheckAntigravityContextLoaded(ctx context.Context, home string, proof AntigravityContextProof, expected string, check AntigravityContextCheck) error {
	current, err := VerifyAntigravityContextLoaded(ctx, home, proof.Fence, expected, check)
	if err != nil {
		return err
	}
	if current != proof {
		return ErrCursorStale
	}
	return nil
}

func unambiguousAntigravityContextRow(line []byte) bool {
	d := json.NewDecoder(bytes.NewReader(line))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return false
		}
		// null and non-string error values cannot masquerade as an absent error.
		if key == "error" && strings.TrimSpace(string(value)) != `""` {
			return false
		}
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') {
		return false
	}
	return d.Decode(new(any)) == io.EOF
}
