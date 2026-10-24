//go:build darwin || linux

package antigravity

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMain's explicit private helper bypasses testing flag parsing so its real
// argv contains ONLY the planned CLI tokens. It is this test binary, not agy.
func TestMain(m *testing.M) {
	if os.Getenv("AG_NATIVE_EXECUTION_PLAN_HELPER") == "1" {
		_, _ = os.Stdout.Write([]byte("ready\n"))
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNativeExecutionPlanReadsOwnedKernelProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "--conversation", conversationA, "--model", "synthetic-model", "--effort", "high", "--mode", "plan")
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=/usr/bin:/bin", "AG_NATIVE_EXECUTION_PLAN_HELPER=1", "AG_SYNTHETIC_PRIVATE_ENV=DO_NOT_EXPOSE_ENVIRONMENT"}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		t.Fatal(err)
	}
	defer func() { input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait(); output.Close() }()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("owned helper not ready", err)
	}
	plan, err := NewNativeProcessReader(nil).ReadAntigravityExecutionPlan(ctx, cmd.Process.Pid)
	if err != nil || plan.Process.PID != cmd.Process.Pid || plan.Process.Executable != executable || plan.WorkspaceCWD != workspace || plan.Mode != "plan" || plan.Model != "synthetic-model" || plan.Effort != "high" || plan.ConversationID != conversationA {
		t.Fatal("kernel plan disagrees with exact owned launch", err)
	}
	want, err := parseNativeCommandLine([]byte(executable+"\x00--conversation\x00"+conversationA+"\x00--model\x00synthetic-model\x00--effort\x00high\x00--mode\x00plan\x00"), executable)
	if err != nil || plan.Digest != want.Digest {
		t.Fatal("kernel digest included non-argv data", err)
	}
}
