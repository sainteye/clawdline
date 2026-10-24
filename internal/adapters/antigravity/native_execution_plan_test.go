//go:build darwin || linux

package antigravity

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func syntheticPlanBytes(args ...string) []byte {
	values := [][]byte{[]byte("/synthetic/agy")}
	for _, arg := range args {
		values = append(values, []byte(arg))
	}
	return append(bytes.Join(values, []byte{0}), 0)
}

func TestNativeExecutionPlanOnlyAdmitsRecognizedArguments(t *testing.T) {
	args := syntheticPlanBytes("--conversation", conversationA, "--model", "synthetic-model", "--effort", "high", "--mode", "plan")
	got, err := parseNativeCommandLine(args, "/synthetic/agy")
	if err != nil || got.Mode != "plan" || got.Model != "synthetic-model" || got.Effort != "high" || got.ConversationID != conversationA || len(got.Digest) != 64 {
		t.Fatal("recognized plan refused", got, err)
	}
	again, err := parseNativeCommandLine(args, "/synthetic/agy")
	if err != nil || again != got {
		t.Fatal("same native bytes changed plan", err)
	}
	changed, err := parseNativeCommandLine(syntheticPlanBytes("--mode", "auto"), "/synthetic/agy")
	if err != nil || changed.Digest == got.Digest {
		t.Fatal("changed plan retained digest", err)
	}
	invalid := map[string][]byte{
		"invalid-utf8":    syntheticPlanBytes("--mode", "plan", "--model", string([]byte{0xff})),
		"missing-mode":    syntheticPlanBytes("--effort", "high"),
		"unknown-mode":    syntheticPlanBytes("--mode", "ask"),
		"duplicate-mode":  syntheticPlanBytes("--mode", "plan", "--mode", "auto"),
		"duplicate-model": syntheticPlanBytes("--mode", "plan", "--model", "a", "--model", "b"),
		"missing-value":   syntheticPlanBytes("--mode"),
		"inline-value":    syntheticPlanBytes("--mode=plan"),
		"sandbox":         syntheticPlanBytes("--mode", "plan", "--sandbox"),
		"prompt":          syntheticPlanBytes("--mode", "plan", "--prompt-interactive", "SYNTHETIC_PRIVATE_PROMPT"),
		"positional":      syntheticPlanBytes("--mode", "plan", "SYNTHETIC_PRIVATE_PROMPT"),
		"unknown-effort":  syntheticPlanBytes("--mode", "plan", "--effort", "ultra"),
		"unknown-id":      syntheticPlanBytes("--mode", "plan", "--conversation", "latest"),
		"flag-as-value":   syntheticPlanBytes("--mode", "plan", "--model", "--sandbox"),
		"control":         syntheticPlanBytes("--mode", "plan", "--model", "synthetic\nprivate"),
		"empty":           syntheticPlanBytes("--mode", "plan", ""),
		"unterminated":    []byte("/synthetic/agy\x00--mode\x00plan"),
		"oversize":        bytes.Repeat([]byte{0}, MaxNativeProcessFileBytes+1),
	}
	for name, data := range invalid {
		t.Run(name, func(t *testing.T) {
			got, err := parseNativeCommandLine(data, "/synthetic/agy")
			requireNativeCode(t, err, "antigravity_native_plan_invalid")
			if got != (nativePlanTokens{}) {
				t.Fatal("refused argv leaked partial plan")
			}
		})
	}
	if _, err := parseNativeCommandLine(args, "/synthetic/another"); err == nil {
		t.Fatal("argv executable mismatch accepted")
	}
}

func TestNativeExecutionPlanBracketsIdentityAndArguments(t *testing.T) {
	for _, kind := range []string{"stable", "process", "workspace", "terminal", "argv", "unreadable", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			sample := nativeSampleFixture(t)
			r := NewNativeProcessReader(func(context.Context, int, string) (string, error) { return "%synthetic-plan", nil })
			reads, plans := 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r.read = func(context.Context, int) (nativeProcessSample, error) {
				reads++
				current := sample
				if reads > 2 {
					switch kind {
					case "process":
						current.generation = "new-generation"
					case "workspace":
						current.WorkspaceCWD = "/synthetic/new-workspace"
					}
				}
				return current, nil
			}
			if kind == "terminal" {
				mapped := 0
				r.resolve = func(context.Context, int, string) (string, error) {
					mapped++
					if mapped > 1 {
						return "%different-plan", nil
					}
					return "%synthetic-plan", nil
				}
			}
			r.readPlan = func(context.Context, int, string) (nativePlanTokens, error) {
				plans++
				if kind == "unreadable" {
					return nativePlanTokens{}, planInvalid()
				}
				if kind == "cancelled" {
					cancel()
				}
				mode := "plan"
				if kind == "argv" && plans == 2 {
					mode = "auto"
				}
				return parseNativeCommandLine(syntheticPlanBytes("--mode", mode), "/synthetic/agy")
			}
			got, err := r.ReadAntigravityExecutionPlan(ctx, sample.Process.PID)
			if kind == "stable" {
				if err != nil || got.Mode != "plan" || got.WorkspaceCWD != sample.WorkspaceCWD || got.Process.KernelGeneration != sample.generation || plans != 2 || reads != 6 {
					t.Fatal("complete plan missing", got, err)
				}
			} else {
				if err == nil || got != (NativeExecutionPlan{}) {
					t.Fatal("changed/unreadable plan escaped", got, err)
				}
				if kind == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
			}
		})
	}
}
