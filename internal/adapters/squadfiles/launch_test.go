package squadfiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublishSquadLaunchKeepsCapabilityPrivateAndSkillsOnDemand(t *testing.T) {
	const launchID = "abcdefghijklmnopqrstuv"
	const capability = "secret-capability"
	document := json.RawMessage(`{"definition_id":"clawdline.persona.architect","scope_id":"project-example","definition":{"version":"1","body":"Role definition"},"handbook":{"text":"Project convention"},"skills":[{"id":"example.skill","version":"1","enabled":true,"content":"Skill body remains on demand","source":"example","digest":"abc"}]}`)
	sum := sha256.Sum256(document)
	dir := t.TempDir()
	files, err := Publish(dir, launchID, hex.EncodeToString(sum[:]), capability, document)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(files.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "Role definition") || !strings.Contains(string(prompt), "Project convention") ||
		!strings.Contains(string(prompt), "example.skill") || strings.Contains(string(prompt), capability) ||
		strings.Contains(string(prompt), "Skill body remains on demand") {
		t.Fatalf("prompt content did not separate identity, secret, and skill: %q", prompt)
	}
	launchDir := filepath.Dir(files.SnapshotPath)
	filesInSkillDir, err := os.ReadDir(filepath.Join(launchDir, "skills"))
	if err != nil || len(filesInSkillDir) != 1 {
		t.Fatalf("skill files = %d, %v", len(filesInSkillDir), err)
	}
	skill, err := os.ReadFile(filepath.Join(launchDir, "skills", filesInSkillDir[0].Name()))
	if err != nil || string(skill) != "Skill body remains on demand" {
		t.Fatalf("skill body = %q, %v", skill, err)
	}
	secret, err := os.ReadFile(files.CapabilityPath)
	if err != nil || string(secret) != capability+"\n" {
		t.Fatalf("capability file = %q, %v", secret, err)
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{files.PromptPath, files.SnapshotPath, files.CapabilityPath} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("private file %s: %v", path, err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("private file %s: %v", path, info.Mode())
			}
		}
	}
	if _, err := Publish(dir, launchID, hex.EncodeToString(sum[:]), capability, document); err == nil {
		t.Fatal("a launch directory was overwritten")
	}
}

func TestPublishSquadLaunchRejectsBadSnapshotWithoutFiles(t *testing.T) {
	dir := t.TempDir()
	document := json.RawMessage(`{"definition_id":"x","scope_id":"s","definition":{"body":"text"}}`)
	if _, err := Publish(dir, "abcdefghijklmnopqrstuv", strings.Repeat("0", 64), "secret", document); err == nil {
		t.Fatal("mismatched digest accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "squad")); !os.IsNotExist(err) {
		t.Fatalf("files written before digest check: %v", err)
	}
}
