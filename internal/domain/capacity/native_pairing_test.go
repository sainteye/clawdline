package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The macOS shell waits this long for the daemon to save a pairing's
// permission before it shows why no code appears
// (shell/darwin/Pairing.swift PairingPermissionClient.saveTimeoutSeconds).
// The Go-only bound scan does not read Swift, so this guard holds the two
// sides together: the constant is the one the request uses, and its value is
// the registered row's.
func TestNativePairingSaveTimeoutMatchesTheShell(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "shell", "darwin", "Pairing.swift"))
	if err != nil {
		t.Fatalf("read Pairing.swift: %v", err)
	}
	src := string(raw)
	m := regexp.MustCompile(`(?m)^\s*static let saveTimeoutSeconds: TimeInterval = ([0-9]+)\s*$`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("Pairing.swift declares no `static let saveTimeoutSeconds: TimeInterval = <n>`")
	}
	if !regexp.MustCompile(`request\.timeoutInterval = Self\.saveTimeoutSeconds\b`).MatchString(src) {
		t.Error("the permission request does not take its timeout from saveTimeoutSeconds")
	}
	want, _ := strconv.ParseInt(m[1], 10, 64)
	for _, e := range Register() {
		if e.Name != ConsoleNativePairingSaveSeconds {
			continue
		}
		if e.Limit != want {
			t.Errorf("%s is %d, Pairing.swift saveTimeoutSeconds says %d", e.Name, e.Limit, want)
		}
		return
	}
	t.Errorf("console.native_pairing_save_seconds (Pairing.swift saveTimeoutSeconds = %d) is not registered", want)
}
