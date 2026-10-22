package squadfiles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestAntigravityContextPublicationIsImmutable(t *testing.T) {
	dir := t.TempDir()
	f := Files{SnapshotPath: filepath.Join(dir, "snapshot.json"), PromptPath: filepath.Join(dir, "prompt.md")}
	c, err := projects.PrepareAntigravityContext(f.PromptPath, "fixed role", "fixed memory", "zh-Hant-TW")
	if err != nil {
		t.Fatal(err)
	}
	p, digest, err := PublishAntigravityContext(f, c)
	if err != nil {
		t.Fatal(err)
	}
	if p2, d2, e := PublishAntigravityContext(f, c); e != nil || p2 != p || d2 != digest {
		t.Fatal(p2, d2, e)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	restored, e := projects.RestoreAntigravityContext(b, digest)
	if e != nil || restored.Prompt != "fixed role" {
		t.Fatal(e)
	}
	if _, e := projects.RestoreAntigravityContext(append(b, ' '), digest); e == nil {
		t.Fatal("changed persisted bytes accepted")
	}
	c, _ = projects.PrepareAntigravityContext(f.PromptPath, "changed role", "fixed memory", "zh-Hant-TW")
	if _, _, e := PublishAntigravityContext(f, c); e == nil {
		t.Fatal("original snapshot overwritten")
	}
}
