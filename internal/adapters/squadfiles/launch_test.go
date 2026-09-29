package squadfiles

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublishSkillFolderCopiesAttachmentsIntoImmutableLaunch(t *testing.T) {
	const launchID = "abcdefghijklmnopqrstuv"
	attachment := []byte{0, 1, 255, 10}
	document, err := json.Marshal(map[string]any{
		"definition_id": "clawdline.persona.architect", "scope_id": "global",
		"definition": map[string]any{"version": "1", "body": "Role definition"},
		"skills": []map[string]any{{"id": "example.folder", "version": "1", "enabled": true,
			"content": "# Skill\n", "folder": true, "source": "imported:project:folder", "digest": "abc",
			"files": []map[string]string{{"path": "references/note.bin", "content_base64": base64.StdEncoding.EncodeToString(attachment)}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(document)
	dir := t.TempDir()
	files, err := Publish(dir, launchID, hex.EncodeToString(sum[:]), "capability", document)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(files.SnapshotPath), "skills"))
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		t.Fatalf("published folders = %v, %v", entries, err)
	}
	root := filepath.Join(filepath.Dir(files.SnapshotPath), "skills", entries[0].Name())
	main, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil || string(main) != "# Skill\n" {
		t.Fatalf("SKILL.md = %q, %v", main, err)
	}
	got, err := os.ReadFile(filepath.Join(root, "references", "note.bin"))
	if err != nil || string(got) != string(attachment) {
		t.Fatalf("attachment = %v, %v", got, err)
	}
	prompt, err := os.ReadFile(files.PromptPath)
	if err != nil || !strings.Contains(string(prompt), filepath.Join(root, "SKILL.md")) {
		t.Fatalf("prompt = %s, %v", prompt, err)
	}
}

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

func TestDisabledSkillStaysInSnapshotButNotInPublishedPromptOrFiles(t *testing.T) {
	dir := t.TempDir()
	document := json.RawMessage(`{"definition_id":"clawdline.persona.backend","scope_id":"global","definition":{"version":"1","body":"Role definition"},"handbook":{"text":""},"skills":[{"id":"user.skill.disabled","version":"1","enabled":false,"content":"disabled body","source":"user-authored","digest":"one"},{"id":"user.skill.enabled","version":"1","enabled":true,"content":"enabled body","source":"user-authored","digest":"two"}]}`)
	sum := sha256.Sum256(document)
	files, err := Publish(dir, "abcdefghijklmnopqrstuv", hex.EncodeToString(sum[:]), "capability", document)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(files.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prompt), "user.skill.disabled") || !strings.Contains(string(prompt), "user.skill.enabled") {
		t.Fatalf("published prompt lists wrong skills: %s", prompt)
	}
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(files.SnapshotPath), "skills"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("published skills = %d, %v", len(entries), err)
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(files.SnapshotPath), "skills", entries[0].Name()))
	if err != nil || string(content) != "enabled body" {
		t.Fatalf("published content = %q, %v", content, err)
	}
	stored, err := os.ReadFile(files.SnapshotPath)
	if err != nil || !strings.Contains(string(stored), `"id":"user.skill.disabled"`) {
		t.Fatalf("snapshot lost disabled skill: %v", err)
	}
	// Enabling the skill later creates a different launch; the first remains
	// immutable for a Session that already owns it.
	enabled := json.RawMessage(strings.Replace(string(document), `"enabled":false`, `"enabled":true`, 1))
	nextSum := sha256.Sum256(enabled)
	next, err := Publish(dir, "abcdefghijklmnopqrstu0", hex.EncodeToString(nextSum[:]), "new-capability", enabled)
	if err != nil {
		t.Fatal(err)
	}
	nextEntries, err := os.ReadDir(filepath.Join(filepath.Dir(next.SnapshotPath), "skills"))
	if err != nil || len(nextEntries) != 2 {
		t.Fatalf("new launch skills = %d, %v", len(nextEntries), err)
	}
	oldEntries, err := os.ReadDir(filepath.Join(filepath.Dir(files.SnapshotPath), "skills"))
	if err != nil || len(oldEntries) != 1 {
		t.Fatalf("old launch changed = %d, %v", len(oldEntries), err)
	}
}
