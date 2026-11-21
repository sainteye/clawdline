package orchestrator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/squadfiles"
	"github.com/sainteye/clawdline/internal/adapters/taskdir"
	"github.com/sainteye/clawdline/internal/domain/squad"
)

// launchBytesRecord is a dispatched child as the fixed launch text sees it:
// a worktree, a root, and a task whose own text is one word, so what is
// measured is what Clawdline adds and not what the root wrote.
func launchBytesRecord() Record {
	return Record{ID: "c0000000-0000-4000-8000-000000000001", Title: "t", Kind: "custom",
		TimeoutMinutes: 120, ProjectDir: "/Users/someone/code/project", Assistant: "claude",
		Instructions: "x", Claims: []string{"docs/a.md"},
		Root:     &RootRef{SessionID: "conv", Assistant: "claude"},
		Worktree: &Worktree{Base: "0000000000000000000000000000000000000001", Branch: "clawdline/task/c0000000-0000-4000-8000-000000000001"}}
}

// squadPromptBytes publishes the built-in persona's launch prompt as a
// dispatch would and answers its size.
func squadPromptBytes(t *testing.T, short string, claims []string) int {
	t.Helper()
	catalog := squad.Builtins()
	var def squad.Definition
	for _, d := range catalog.Definitions {
		if d.ShortID == short {
			def = d
		}
	}
	skills := []map[string]any{}
	for _, ref := range def.Skills {
		for _, s := range catalog.Skills {
			if s.SkillID == ref.ID {
				skills = append(skills, map[string]any{"id": s.SkillID, "version": s.Version, "enabled": true,
					"name": s.Name, "purpose": s.Purpose, "content": s.Content, "source": s.Source, "digest": s.Digest})
			}
		}
	}
	document, err := json.Marshal(map[string]any{"definition_id": def.DefinitionID, "scope_id": "project-00000000000000000000000a",
		"definition": map[string]any{"version": def.Version, "body": def.Body}, "skills": skills})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(document)
	stateDir := t.TempDir()
	files, err := squadfiles.PublishFor(stateDir, "abcdefghijklmnopqrstuv", hex.EncodeToString(sum[:]), "capability", document,
		squadfiles.Task{Claims: claims})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(files.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	// Measure fixed product text against the same public fixture root as the
	// child brief. A managed TMPDIR's spelling is not launch-text growth.
	body = bytes.ReplaceAll(body, []byte(stateDir), []byte("/Users/someone/.config/clawdline-next"))
	return len(body)
}

// The fixed text Clawdline adds to one dispatched child, in bytes.
//
// Measured on 2026-10-02 before CHILD.md lost the shared protocol and the
// squad prompt lost its repeated provenance and usage text: CHILD.md 8664,
// the backend prompt 6557 whatever the task wrote, a role with no skill
// (seo) 6004. After: CHILD.md 5432, the backend prompt 5798 for a task that
// writes Go and 5063 for one that writes only docs, seo 5067. The bounds are
// those numbers rounded up, so the text cannot grow back unnoticed.
//
// On 2026-11-08 backend gained a second default skill, api-and-interface-design,
// which applies to any write: its prompt measured 6479 bytes writing Go and
// 6128 writing only docs, the docs case now carrying the usage paragraph and
// one entry instead of one line. seo gained skills too, so a role with none is
// measured with pricing (4683 bytes).
func TestTheFixedLaunchAdditionsAreMeasured(t *testing.T) {
	b := launchBytesBroker(t, "en")
	r := launchBytesRecord()
	child := len(b.ChildBrief(r, "/Users/someone/.config/clawdline-next/worktrees/clawdline-0000000a/"+r.ID))
	backendGo := squadPromptBytes(t, "backend", []string{"internal/a.go"})
	backendDocs := squadPromptBytes(t, "backend", []string{"docs/a.md"})
	noSkill := squadPromptBytes(t, "pricing", nil)
	t.Logf("CHILD.md %d bytes; backend squad prompt %d bytes writing Go, %d writing only docs; pricing (no skill) %d bytes",
		child, backendGo, backendDocs, noSkill)
	for _, m := range []struct {
		name       string
		got, bound int
	}{{"CHILD.md", child, 5500}, {"backend writing Go", backendGo, 6550}, {"backend writing docs", backendDocs, 6200}, {"pricing", noSkill, 4750}} {
		if m.got > m.bound {
			t.Errorf("%s is %d bytes, past the %d it was cut to", m.name, m.got, m.bound)
		}
	}
	if guide := len(ChildGuide()); guide == 0 {
		t.Error("clawdline guide child is empty")
	}
	for _, lang := range []string{"en", "zh-Hant", "zh-TW"} {
		t.Run(lang, func(t *testing.T) {
			b := launchBytesBroker(t, lang)
			n := len(b.ChildBrief(r, "/Users/someone/.config/clawdline-next/worktrees/clawdline-0000000a/"+r.ID))
			t.Logf("%s CHILD.md %d bytes", lang, n)
			if n > 5500 {
				t.Fatalf("localized CHILD.md is %d bytes, past 5500", n)
			}
		})
	}
}

func launchBytesBroker(t *testing.T, language string) *Broker {
	t.Helper()
	b := &Broker{Dir: t.TempDir(), Tasks: taskdir.New("/Users/someone/.config/clawdline-next"), Executable: "/Applications/Clawdline Next.app/Contents/MacOS/clawdline", Port: 7727}
	if _, err := nextconfig.Open(b.Dir).Set(map[string]any{"language": language}); err != nil {
		t.Fatal(err)
	}
	return b
}

// The reviewed ordinary briefing includes scratch retention and separation.
// Image-review opt-out must keep that task and protocol baseline unchanged.
func TestImageReviewDoesNotChangeTheExistingChildBrief(t *testing.T) {
	b := launchBytesBroker(t, "en")
	r := launchBytesRecord()
	sum := sha256.Sum256([]byte(b.ChildBrief(r, "/Users/someone/.config/clawdline-next/worktrees/clawdline-0000000a/"+r.ID)))
	if got := hex.EncodeToString(sum[:]); got != "a356c67131488d323ec066605e8f4e81ec903f6246fd8ed4d0877faeee39cf37" {
		t.Fatalf("reviewed ordinary CHILD.md changed: %s", got)
	}
}
