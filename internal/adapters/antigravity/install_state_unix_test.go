//go:build darwin || linux

package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallForStateReachesOnlyItsNamedPrivateRoot(t *testing.T) {
	parent := privateRuntimeRoot(t)
	root := filepath.Join(parent, "config with ' quote; $(touch should-not-exist)")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := installWorkspace(t, `{"other":{"command":"keep this"}}`)
	helper := filepath.Join(parent, "helper with ' quote")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$CLAWDLINE_NEXT_DIR\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	p, err := PreviewInstallForState(workspace, helper, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectInstallForState(workspace, helper, root); err == nil {
		t.Fatal("preview became installed authority")
	}
	if err := ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	got, err := InspectInstallForState(workspace, helper, root)
	if err != nil || got.Digest == "" || got.PlanDigest == "" {
		t.Fatal(got, err)
	}
	doc, _ := rawObject(p.After)
	owned, _ := rawObject(doc["clawdline"])
	var hooks []struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(owned["PreInvocation"], &hooks); err != nil || len(hooks) != 1 {
		t.Fatal(hooks, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), MaxProcessReadTime)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", hooks[0].Command)
	cmd.Dir = parent
	cmd.Env = append(os.Environ(), "CLAWDLINE_NEXT_DIR=/synthetic-wrong-root")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	want := root + "\nprovider\nobserve\nantigravity\n--event\nPreInvocation\n"
	if string(output) != want {
		t.Fatalf("callback root/arguments: %q", output)
	}
	if _, err := os.Lstat(filepath.Join(parent, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("config root executed shell text", err)
	}
	otherRoot := privateRuntimeRoot(t)
	if _, err := InspectInstallForState(workspace, helper, otherRoot); err == nil {
		t.Fatal("another daemon borrowed managed hooks")
	}
	plan, err := ManagedHookPlanDigest(helper, root)
	if err != nil || got.PlanDigest != plan {
		t.Fatal(plan, err)
	}
	// Workspace-specific foreign settings change the whole-file digest, not
	// the managed plan or consent candidate for the same helper/root.
	path := filepath.Join(workspace, ".agents", "hooks.json")
	doc["other"] = json.RawMessage(`{"command":"changed foreign hook"}`)
	if err := os.WriteFile(path, writeRawObject(doc), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := InspectInstallForState(workspace, helper, root)
	if err != nil || changed.PlanDigest != got.PlanDigest || changed.Digest == got.Digest {
		t.Fatal(changed, err)
	}
	remove, err := PreviewInstallForState(workspace, helper, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyInstall(remove); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, doc["other"]) {
		t.Fatal("removal changed foreign hook")
	}
	if _, err := InspectInstallForState(workspace, helper, root); err == nil {
		t.Fatal("removed hooks remained installed")
	}
}

func TestInstallForStateRefusesUnverifiedOrChangedConfiguration(t *testing.T) {
	root := privateRuntimeRoot(t)
	for _, kind := range []string{"missing", "disabled", "partial", "unknown-owned", "changed-file", "changed-root", "forged-root"} {
		t.Run(kind, func(t *testing.T) {
			workspace := installWorkspace(t, "")
			helper := "/synthetic/helper"
			p, err := PreviewInstallForState(workspace, helper, root, false)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "missing" {
				if _, err := InspectInstallForState(workspace, helper, root); err == nil {
					t.Fatal("absent hooks accepted")
				}
				return
			}
			if kind == "changed-root" || kind == "forged-root" {
				if kind == "changed-root" {
					p.ConfigRoot = privateRuntimeRoot(t)
				} else {
					p.ConfigRoot = "relative"
				}
				if err := ApplyInstall(p); err == nil {
					t.Fatal("forged state root installed")
				}
				return
			}
			if err := ApplyInstall(p); err != nil {
				t.Fatal(err)
			}
			doc, _ := rawObject(p.After)
			owned, _ := rawObject(doc["clawdline"])
			switch kind {
			case "disabled":
				owned["enabled"] = json.RawMessage("false")
			case "partial":
				delete(owned, "Stop")
			case "unknown-owned":
				owned["future"] = json.RawMessage("true")
			case "changed-file":
				doc["foreign"] = json.RawMessage("true")
				if err := os.WriteFile(p.Path, writeRawObject(doc), 0600); err != nil {
					t.Fatal(err)
				}
				if err := ApplyInstall(p); err == nil {
					t.Fatal("stale preview overwrote editor")
				}
				return
			}
			doc["clawdline"] = writeRawObject(owned)
			if err := os.WriteFile(p.Path, writeRawObject(doc), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectInstallForState(workspace, helper, root); err == nil {
				t.Fatal("invalid owned block accepted")
			}
		})
	}
	for _, bad := range []string{"relative", root + "/../other", root + "\n"} {
		if _, err := ManagedHookPlanDigest("/synthetic/helper", bad); err == nil {
			t.Fatal("invalid config root accepted")
		}
	}
	if _, err := ManagedHookPlanDigest("relative", root); err == nil {
		t.Fatal("relative helper accepted")
	}
	// Existing default-root helper bytes cannot silently become state-bound.
	workspace := installWorkspace(t, "")
	p, err := PreviewInstall(workspace, "/synthetic/helper", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyInstall(p); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectInstallForState(workspace, "/synthetic/helper", root); err == nil {
		t.Fatal("legacy default-root hook accepted")
	}
	if strings.Contains(string(p.After), "CLAWDLINE_NEXT_DIR") {
		t.Fatal("legacy fixture contract changed")
	}
}
