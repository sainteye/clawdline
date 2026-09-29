package squadpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testIcon() Icon {
	icon := Icon{Accent: "#234567", Cells: make([][]*string, 7)}
	for i := range icon.Cells {
		icon.Cells[i] = make([]*string, 8)
	}
	return icon
}

func example() (Manifest, map[string][]byte) {
	icon := testIcon()
	m := Manifest{Namespace: "example.squad", Source: "example", License: "MIT",
		Teams: []Definition{
			{ID: "example.squad.team.alpha", Version: "1.0.0", Name: "Alpha", Icon: icon, Body: "definitions/team/alpha.md", Personas: []string{"example.squad.persona.writer", "example.squad.persona.reviewer"}},
			{ID: "example.squad.team.beta", Version: "1.0.0", Name: "Beta", Icon: icon, Body: "definitions/team/beta.md", Personas: []string{"example.squad.persona.reviewer", "example.squad.persona.writer"}},
		},
		Personas: []Definition{
			{ID: "example.squad.persona.writer", Version: "1.0.0", Name: "Writer", Icon: icon, Body: "definitions/persona/writer.md", Skills: []string{"example.squad.skill.draft", "example.squad.skill.review"}},
			{ID: "example.squad.persona.reviewer", Version: "1.0.0", Name: "Reviewer", Icon: icon, Body: "definitions/persona/reviewer.md", Skills: []string{"example.squad.skill.review", "example.squad.skill.draft"}},
		},
		Skills: []Definition{
			{ID: "example.squad.skill.draft", Version: "1.0.0", Name: "Draft", Icon: icon, Body: "definitions/skill/draft.md"},
			{ID: "example.squad.skill.review", Version: "1.0.0", Name: "Review", Icon: icon, Body: "definitions/skill/review.md"},
		},
	}
	files := map[string][]byte{}
	for _, defs := range [][]Definition{m.Teams, m.Personas, m.Skills} {
		for _, d := range defs {
			files[d.Body] = []byte("# " + d.Name + "\nInstructions are data.\n")
		}
	}
	return m, files
}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) {
		t.Fatalf("expected typed refusal, got %v", err)
	}
	return r.Code
}

func TestExampleRoundTripPreservesIdentityOrderAndReferences(t *testing.T) {
	m, files := example()
	archive, err := Build(m, files)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Parse(archive)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest == "" || len(first.Manifest.Teams) != 2 || len(first.Manifest.Personas) != 2 || len(first.Manifest.Skills) != 2 {
		t.Fatalf("incomplete package: %+v", first.Manifest)
	}
	again, err := Build(first.Manifest, first.Files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(again)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Manifest, second.Manifest) || !reflect.DeepEqual(first.Files, second.Files) || first.Digest != second.Digest {
		t.Fatal("a re-export changed IDs, versions, icons, order, references or bytes")
	}
}

func TestCommittedExampleArchiveCanBeReadAndReexported(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", "ai-squad-example.zip"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Manifest.Teams) != 2 || len(p.Manifest.Personas) != 2 || len(p.Manifest.Skills) != 2 {
		t.Fatal("example archive lost its multi-team definitions")
	}
	again, err := Export(p.Manifest, p.Files, ExportSelection{})
	if err != nil {
		t.Fatal(err)
	}
	q, err := Parse(again)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Manifest, q.Manifest) || !reflect.DeepEqual(p.Files, q.Files) {
		t.Fatal("example export changed identities, icons, order or references")
	}
}

func rawZIP(entries []struct {
	name string
	body []byte
	mode os.FileMode
}) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		} else {
			h.SetMode(0644)
		}
		f, _ := w.CreateHeader(h)
		_, _ = f.Write(e.body)
	}
	_ = w.Close()
	return b.Bytes()
}

func rawManifest(m Manifest, files map[string][]byte) []byte {
	m.Format, m.Version = Format, Version
	for _, defs := range [][]Definition{m.Teams, m.Personas, m.Skills} {
		for i := range defs {
			defs[i].SHA256 = digestOf(files[defs[i].Body])
		}
	}
	for i := range m.Private {
		m.Private[i].SHA256 = digestOf(files[m.Private[i].Path])
	}
	b, _ := json.Marshal(m)
	return b
}

func rawPackage(m Manifest, files map[string][]byte) []byte {
	entries := []struct {
		name string
		body []byte
		mode os.FileMode
	}{{"manifest.json", rawManifest(m, files), 0644}}
	for name, body := range files {
		entries = append(entries, struct {
			name string
			body []byte
			mode os.FileMode
		}{name, body, 0644})
	}
	return rawZIP(entries)
}

func TestRejectsSemanticAndPrivacyBoundaryErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Manifest, map[string][]byte)
		code   string
	}{
		{"duplicate ID", func(m *Manifest, _ map[string][]byte) { m.Personas[1].ID = m.Personas[0].ID }, ErrDuplicateID},
		{"missing skill", func(m *Manifest, _ map[string][]byte) { m.Personas[0].Skills[0] = "example.squad.skill.gone" }, ErrMissingReference},
		{"duplicate reference", func(m *Manifest, _ map[string][]byte) { m.Personas[0].Skills[1] = m.Personas[0].Skills[0] }, ErrDuplicateReference},
		{"same name", func(m *Manifest, _ map[string][]byte) { m.Personas[1].Name = m.Personas[0].Name }, ErrNameConflict},
		{"reserved ID", func(m *Manifest, _ map[string][]byte) { m.Personas[0].ID = "clawdline.persona.security" }, ErrReservedID},
		{"digest mismatch", func(_ *Manifest, f map[string][]byte) { f["definitions/skill/draft.md"] = []byte("changed") }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, files := example()
			manifest := rawManifest(m, files)
			tc.mutate(&m, files)
			if tc.name == "digest mismatch" {
				entries := []struct {
					name string
					body []byte
					mode os.FileMode
				}{{"manifest.json", manifest, 0644}}
				for name, body := range files {
					entries = append(entries, struct {
						name string
						body []byte
						mode os.FileMode
					}{name, body, 0644})
				}
				_, err := Parse(rawZIP(entries))
				if got := refusalCode(t, err); got != ErrDigestMismatch {
					t.Fatalf("got %s", got)
				}
				return
			}
			_, err := Parse(rawPackage(m, files))
			if got := refusalCode(t, err); got != tc.code {
				t.Fatalf("got %s, want %s", got, tc.code)
			}
		})
	}
}

func TestRejectsHostileZIPProfiles(t *testing.T) {
	m, files := example()
	manifest := rawManifest(m, files)
	base := []struct {
		name string
		body []byte
		mode os.FileMode
	}{{"manifest.json", manifest, 0644}}
	for name, body := range files {
		base = append(base, struct {
			name string
			body []byte
			mode os.FileMode
		}{name, body, 0644})
	}
	for _, tc := range []struct {
		name, path, code string
		mode             os.FileMode
	}{
		{"traversal", "../outside.md", ErrArchivePath, 0644},
		{"absolute", "/outside.md", ErrArchivePath, 0644},
		{"backslash", "definitions\\outside.md", ErrArchivePath, 0644},
		{"drive", "c:outside.md", ErrArchivePath, 0644},
		{"symlink", "definitions/skill/link.md", ErrArchiveEntryType, os.ModeSymlink | 0777},
		{"extra", "definitions/skill/extra.md", ErrArchiveExtraFile, 0644},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries := append([]struct {
				name string
				body []byte
				mode os.FileMode
			}(nil), base...)
			entries = append(entries, struct {
				name string
				body []byte
				mode os.FileMode
			}{tc.path, []byte("x"), tc.mode})
			_, err := Parse(rawZIP(entries))
			if got := refusalCode(t, err); got != tc.code {
				t.Fatalf("got %s, want %s", got, tc.code)
			}
		})
	}
	entries := append([]struct {
		name string
		body []byte
		mode os.FileMode
	}(nil), base...)
	entries = append(entries, struct {
		name string
		body []byte
		mode os.FileMode
	}{"MANIFEST.JSON", []byte("x"), 0644})
	if _, err := Parse(rawZIP(entries)); refusalCode(t, err) != ErrArchivePathCollision {
		t.Fatal(err)
	}
	entries = append(entries[:len(base):len(base)], base[0])
	if _, err := Parse(rawZIP(entries)); refusalCode(t, err) != ErrArchivePathCollision {
		t.Fatal(err)
	}
}

func TestRejectsOldFormatDuplicateJSONAndExpansion(t *testing.T) {
	m, files := example()
	archive := rawPackage(m, files)
	parsed, err := Parse(archive)
	if err != nil {
		t.Fatal(err)
	}
	_ = parsed
	manifest := rawManifest(m, files)
	old := bytes.Replace(manifest, []byte(`"version":1`), []byte(`"version":0`), 1)
	entries := []struct {
		name string
		body []byte
		mode os.FileMode
	}{{"manifest.json", old, 0644}}
	for name, body := range files {
		entries = append(entries, struct {
			name string
			body []byte
			mode os.FileMode
		}{name, body, 0644})
	}
	if _, err := Parse(rawZIP(entries)); refusalCode(t, err) != ErrFormatUnsupported {
		t.Fatal(err)
	}
	dup := bytes.Replace(manifest, []byte(`"format":`), []byte(`"format":"bad","format":`), 1)
	entries[0].body = dup
	if _, err := Parse(rawZIP(entries)); refusalCode(t, err) != ErrManifestInvalid {
		t.Fatal(err)
	}
	files["definitions/skill/draft.md"] = []byte(strings.Repeat("A", MaxFileBytes))
	if _, err := Parse(rawPackage(m, files)); refusalCode(t, err) != ErrArchiveExpansion {
		t.Fatal(err)
	}
	files["definitions/skill/draft.md"] = []byte(strings.Repeat("A", MaxFileBytes+1))
	if _, err := Parse(rawPackage(m, files)); refusalCode(t, err) != ErrArchiveFileTooLarge {
		t.Fatal(err)
	}
}

func TestRejectsLocalHeaderMismatch(t *testing.T) {
	m, files := example()
	archive := rawPackage(m, files)
	archive[8] ^= 1 // local method differs from the central directory
	if _, err := Parse(archive); refusalCode(t, err) != ErrArchiveHeaderMismatch {
		t.Fatal(err)
	}
}

func TestExportDefaultsToNoPrivateBytesAndSelectsOnlyConfirmedScope(t *testing.T) {
	m, files := example()
	m.Private = []Private{
		{Scope: "global", Path: "private/global.json"},
		{Scope: "repo", Project: "project-aaa", Path: "private/repo-a.json"},
		{Scope: "repo", Project: "project-bbb", Path: "private/repo-b.json"},
	}
	files["private/global.json"] = []byte(`{"settings":[{"definition_id":"example.squad.persona.writer","payload":{"handbook":"GLOBAL_SECRET_MARKER"}}]}`)
	files["private/repo-a.json"] = []byte(`{"settings":[{"definition_id":"example.squad.persona.writer","payload":{"handbook":"PROJECT_A_SECRET_MARKER"}}]}`)
	files["private/repo-b.json"] = []byte(`{"settings":[{"definition_id":"example.squad.persona.writer","payload":{"handbook":"PROJECT_B_SECRET_MARKER"}}]}`)
	public, err := Export(m, files, ExportSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(public, []byte("SECRET_MARKER")) {
		t.Fatal("private bytes appeared in default ZIP")
	}
	parsed, err := Parse(public)
	if err != nil || len(parsed.Manifest.Private) != 0 {
		t.Fatalf("default private content: %v, %+v", err, parsed.Manifest.Private)
	}
	if _, err := Export(m, files, ExportSelection{PrivateScopes: []string{"project-aaa"}}); refusalCode(t, err) != ErrPrivateConfirmation {
		t.Fatal(err)
	}
	selected, err := Export(m, files, ExportSelection{PrivateScopes: []string{"project-aaa"}, ConfirmPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = Parse(selected)
	if err != nil {
		t.Fatal(err)
	}
	_, hasB := parsed.Files["private/repo-b.json"]
	_, hasGlobal := parsed.Files["private/global.json"]
	if len(parsed.Manifest.Private) != 1 || parsed.Manifest.Private[0].Project != "project-aaa" ||
		!bytes.Contains(parsed.Files["private/repo-a.json"], []byte("PROJECT_A_SECRET_MARKER")) || hasB || hasGlobal {
		t.Fatal("selected export crossed a private scope")
	}
}
