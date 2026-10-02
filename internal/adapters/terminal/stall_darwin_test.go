//go:build darwin

package terminal

import "testing"

func TestTheSampledProcessIsITermNotItsHelpers(t *testing.T) {
	table := `  684 /Applications/iTerm.app/Contents/XPCServices/pidinfo.xpc/Contents/MacOS/pidinfo
  711 /Users/someone/Library/Application Support/iTerm2/iTermServer-3.7.0
15376 /Applications/iTerm.app/Contents/XPCServices/iTerm2SandboxedWorker.xpc/Contents/MacOS/iTerm2SandboxedWorker
  602 /Applications/iTerm.app/Contents/MacOS/iTerm2
`
	if got := itermPID(table); got != 602 {
		t.Fatalf("pid %d, want 602", got)
	}
	if got := itermPID("  1 /sbin/launchd\n"); got != 0 {
		t.Fatalf("pid %d with no iTerm2 running, want 0", got)
	}
}

func TestOnlyThisDaemonsOwnOsascriptsAreCounted(t *testing.T) {
	table := `  900   500    00:07 /usr/bin/osascript
  901   500    00:01 osascript
  902   777    00:09 /usr/bin/osascript
  903   500    00:02 /bin/ps
`
	want := "count: 2\npid 900 running for 00:07\npid 901 running for 00:01\n"
	if got := ownOsascriptRows(table, 500); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
