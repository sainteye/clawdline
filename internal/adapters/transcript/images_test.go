package transcript

import (
	"strings"
	"testing"
)

const (
	idA = "1a000000-0000-4000-8000-000000000002"
	idB = "1a000000-0000-4000-8000-000000000003"
)

func marker(id string) string { return `<clawdline-image id="` + id + `">` }

// `SessionImageMarker.read`, case by case: what is lifted out, what stays, and
// what the prose looks like afterwards.
func TestReadImageMarkers(t *testing.T) {
	cases := []struct {
		name, in, text string
		ids            []string
		limit          int
	}{
		{"alone on its line", "Here:\n" + marker(idA) + "\nDone.", "Here:\nDone.", []string{idA}, 6},
		{"between paragraphs", "One.\n\n" + marker(idA) + "\n\nTwo.", "One.\n\nTwo.", []string{idA}, 6},
		{"beside words", "See " + marker(idA) + " there", "See  there", []string{idA}, 6},
		{"the whole turn", marker(idA), "", []string{idA}, 6},
		{"indented, trailing spaces", "a\n  " + marker(idA) + "  \nb", "a\nb", []string{idA}, 6},
		{"two on a line", marker(idA) + " " + marker(idB) + "\nz", " \nz", []string{idA, idB}, 6},
		{"uppercase id stays", `x <clawdline-image id="1A000000-0000-4000-8000-000000000002">`, `x <clawdline-image id="1A000000-0000-4000-8000-000000000002">`, nil, 6},
		{"placeholder stays", `<clawdline-image id="ARTIFACT_ID">`, `<clawdline-image id="ARTIFACT_ID">`, nil, 6},
		{"unclosed stays", `<clawdline-image id="` + idA, `<clawdline-image id="` + idA, nil, 6},
		{"a bad one does not hide a good one", `<clawdline-image id="x"> ` + marker(idA), `<clawdline-image id="x"> `, []string{idA}, 6},
		{"fenced stays", "```\n" + marker(idA) + "\n```\n" + marker(idB), "```\n" + marker(idA) + "\n```\n", []string{idB}, 6},
		{"unclosed fence runs to the end", "~~~md\n" + marker(idA), "~~~md\n" + marker(idA), nil, 6},
		{"over the limit stays", marker(idA) + "\n" + marker(idB), marker(idB), []string{idA}, 1},
		{"no budget", marker(idA), marker(idA), nil, 0},
	}
	for _, c := range cases {
		text, ids := readImageMarkers(c.in, c.limit)
		if text != c.text || strings.Join(ids, ",") != strings.Join(c.ids, ",") {
			t.Fatalf("%s: %q %v", c.name, text, ids)
		}
	}
}

// A Claude turn spends one budget of six across its blocks; a Codex message
// is read the same way; and a turn that is only a picture is still an entry.
func TestAssistantTurnsCarryTheirPictures(t *testing.T) {
	seven := strings.Repeat(marker(idA)+"\n", 4)
	path := writeRecord(t,
		claudeRow("assistant", []any{
			m{"type": "text", "text": seven},
			m{"type": "text", "text": "and\n" + strings.Repeat(marker(idB)+"\n", 3)},
		}),
		claudeRow("assistant", marker(idA)),
	)
	page, _ := ReadClaude(path, 10)
	if len(page.Entries) != 3 {
		t.Fatalf("%+v", page.Entries)
	}
	if n := len(page.Entries[0].ArtifactIDs); n != 4 || page.Entries[0].Text != "" {
		t.Fatalf("first block: %+v", page.Entries[0])
	}
	second := page.Entries[1]
	if len(second.ArtifactIDs) != 2 || second.Text != "and\n"+marker(idB) {
		t.Fatalf("the budget is the turn's: %q %v", second.Text, second.ArtifactIDs)
	}
	if last := page.Entries[2]; last.Kind != KindAssistant || last.Text != "" || len(last.ArtifactIDs) != 1 {
		t.Fatalf("a picture alone: %+v", last)
	}
	if text, ids := readImageMarkers("Look "+marker(idA), 6); text != "Look " || len(ids) != 1 {
		t.Fatal(text)
	}
	e, ok := assistantEntry("  "+marker(idB)+"\n", 5, 6)
	if !ok || e.Text != "" || e.ArtifactIDs[0] != idB {
		t.Fatalf("%+v", e)
	}
}

// A version-2 session message is recognised only with valid references, and
// carries them.
func TestSessionMessageVersionTwo(t *testing.T) {
	ref := `{"byte_count":81237,"expires_at":1789698504,"height":358,"id":"` + idA + `","media_type":"image/png","width":1192}`
	envelope := func(artifacts string) string {
		return `<clawdline-message>{"artifacts":[` + artifacts + `],"body":"look","kind":"session_message","protocol":"clawdline.message","source":{"assistant":"claude","id":"%1","label":"Root"},"version":2}</clawdline-message>`
	}
	e, ok := sessionMessage(envelope(ref), 1)
	if !ok || len(e.Artifacts) != 1 || e.Artifacts[0].Width != 1192 || e.Artifacts[0].ID != idA {
		t.Fatalf("%v %+v", ok, e)
	}
	for _, bad := range []string{
		``,
		strings.Replace(ref, `"image/png"`, `"image/jpeg"`, 1),
		strings.Replace(ref, `1192`, `true`, 1),
		strings.Replace(ref, `1192`, `0`, 1),
		strings.Replace(ref, `"width":1192`, `"width":1192,"state":"expired"`, 1),
		strings.Replace(ref, idA, strings.ToUpper(idA), 1),
		strings.Repeat(ref+",", 6) + ref,
	} {
		if _, ok := sessionMessage(envelope(bad), 1); ok {
			t.Fatalf("taken: %s", bad)
		}
	}
}
