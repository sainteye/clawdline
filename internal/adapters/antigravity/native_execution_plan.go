package antigravity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/ports"
)

// MaxNativeProcessFileBytes bounds each kernel process-file/buffer read.
// Overflow refuses rather than truncating into a plausible observation. This
// existing registered process-file budget also covers native execution plans.
const MaxNativeProcessFileBytes = 1 << 20

type nativePlanTokens struct {
	Mode, Model, Effort, ConversationID, Digest string
}

// NativeExecutionPlan contains only recognized launch tokens plus their
// digest. No raw argv, environment or inline prompt leaves the reader. This is
// observed startup intent, NOT proof of effective mode or tool enforcement.
type NativeExecutionPlan struct {
	Process                                     ports.AntigravityProcess
	WorkspaceCWD                                string
	Mode, Model, Effort, ConversationID, Digest string
}

func planInvalid() error { return nativeIdentityError("antigravity_native_plan_invalid", nil) }
func planToken(s string) bool {
	return s != "" && utf8.ValidString(s) && !strings.HasPrefix(s, "-") && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// parseNativeArguments consumes only argc argv values. The platform owns and
// clears the kernel buffer, including any Darwin ABI environment tail.
func parseNativeArguments(next func() ([]byte, bool), executable string) (nativePlanTokens, error) {
	var result nativePlanTokens
	first, ok := next()
	if !ok || string(first) != executable || !safePath(executable) {
		return result, planInvalid()
	}
	digest := sha256.New()
	add := func(value []byte) { _, _ = digest.Write(value); _, _ = digest.Write([]byte{0}) }
	add(first)
	for {
		flag, ok := next()
		if !ok {
			break
		}
		value, ok := next()
		if !ok {
			return nativePlanTokens{}, planInvalid()
		}
		word := string(value)
		if !planToken(word) {
			return nativePlanTokens{}, planInvalid()
		}
		var target *string
		switch string(flag) {
		case "--mode":
			if word != "accept-edits" && word != "auto" && word != "plan" {
				return nativePlanTokens{}, planInvalid()
			}
			target = &result.Mode
		case "--model":
			target = &result.Model
		case "--effort":
			switch word {
			case "low", "medium", "high", "xhigh", "max":
			default:
				return nativePlanTokens{}, planInvalid()
			}
			target = &result.Effort
		case "--conversation":
			if !validID(word) {
				return nativePlanTokens{}, planInvalid()
			}
			target = &result.ConversationID
		default:
			return nativePlanTokens{}, planInvalid()
		}
		if *target != "" {
			return nativePlanTokens{}, planInvalid()
		}
		*target = word
		add(flag)
		add(value)
	}
	if result.Mode == "" {
		return nativePlanTokens{}, planInvalid()
	}
	result.Digest = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

func parseNativeCommandLine(data []byte, executable string) (nativePlanTokens, error) {
	if len(data) == 0 || len(data) > MaxNativeProcessFileBytes || data[len(data)-1] != 0 {
		return nativePlanTokens{}, planInvalid()
	}
	return parseNativeArguments(func() ([]byte, bool) {
		if len(data) == 0 {
			return nil, false
		}
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, false
		} // complete termination checked above
		word := data[:end]
		data = data[end+1:]
		return word, true
	}, executable)
}

// ReadAntigravityExecutionPlan brackets two bounded argument observations with
// complete identity/workspace/terminal reads. Mutation, exit, reuse or unknown
// argv refuses the entire result. There is no retry or subprocess invocation.
func (r *NativeProcessReader) ReadAntigravityExecutionPlan(ctx context.Context, pid int) (NativeExecutionPlan, error) {
	if r == nil || r.readPlan == nil {
		return NativeExecutionPlan{}, nativeIdentityError("antigravity_native_plan_unsupported", nil)
	}
	before, err := r.ReadAntigravityProcessWithWorkspace(ctx, pid)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	first, err := r.readPlan(ctx, pid, before.Process.Executable)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	middle, err := r.ReadAntigravityProcessWithWorkspace(ctx, pid)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	second, err := r.readPlan(ctx, pid, middle.Process.Executable)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	after, err := r.ReadAntigravityProcessWithWorkspace(ctx, pid)
	if err != nil {
		return NativeExecutionPlan{}, err
	}
	if before != middle || before != after || first != second || first.Mode == "" || len(first.Digest) != sha256.Size*2 {
		return NativeExecutionPlan{}, nativeIdentityError("antigravity_native_plan_changed", nil)
	}
	if err := nativeContext(ctx); err != nil {
		return NativeExecutionPlan{}, err
	}
	return NativeExecutionPlan{Process: after.Process, WorkspaceCWD: after.WorkspaceCWD,
		Mode: first.Mode, Model: first.Model, Effort: first.Effort, ConversationID: first.ConversationID, Digest: first.Digest}, nil
}
