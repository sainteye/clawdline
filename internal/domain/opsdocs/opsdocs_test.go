package opsdocs

import (
	"strings"
	"testing"
)

// The console shows the server's type, so this table is the whole rule.
// Every id below is made up.
func TestALinksTypeIsReadFromItsURLAlone(t *testing.T) {
	for _, tc := range []struct{ url, want string }{
		{"https://app.notion.com/p/Runbook-0123456789abcdef0123456789abcdef", "notion_page"},
		{"https://www.notion.so/team/Runbook-0123456789abcdef0123456789abcdef", "notion_page"},
		{"https://notion.so/Runbook-0123456789abcdef0123456789abcdef", "notion_page"},
		{"https://example-team.notion.site/Runbook-0123456789abcdef", "notion_page"},
		{"https://docs.google.com/document/d/FAKE-doc-id_123/edit?tab=t.0#heading=h.x", "google_doc"},
		{"https://docs.google.com/spreadsheets/d/FAKE-sheet-id/edit#gid=0", "google_sheet"},
		{"https://docs.google.com/presentation/d/FAKE-deck-id/edit", "google_slides"},
		{"https://docs.google.com/forms/d/e/FAKE-form-id/viewform", "google_form"},
		{"https://drive.google.com/drive/folders/FAKE-folder-id", "google_drive"},
		{"https://docs.google.com/other/FAKE", "web"},
		{"https://notion.so.example.com/page", "web"},
		{"http://example.com/handbook", "web"},
	} {
		_, got, err := ClassifyURL(tc.url)
		if err != nil || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.url, got, err, tc.want)
		}
	}
}

func TestAnythingButAnHTTPLinkIsRefused(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)", "file:///etc/passwd", "ftp://example.com/x", "notion.so/page",
		"", "https://", "https://someone@example.com/", "https://example.com/a b",
		"https://example.com/" + strings.Repeat("a", URLBytesLimit),
	} {
		if _, _, err := ClassifyURL(raw); err != ErrBadURL {
			t.Errorf("%q: accepted (%v)", raw, err)
		}
	}
}

func TestANewMarkdownNameIsOnePlainSegment(t *testing.T) {
	if got, err := MarkdownFileName(" Release checklist "); err != nil || got != "Release checklist.md" {
		t.Fatalf("plain name: %q %v", got, err)
	}
	for _, bad := range []string{"", "a/b", `a\b`, ".hidden", "..", "tab\there", strings.Repeat("x", MarkdownNameBytesLimit)} {
		if _, err := MarkdownFileName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTheResolveTodoNamesTheCommandAndFitsItsLimit(t *testing.T) {
	text := ResolveTodoText("ope_0123456789abcdef01234567", "https://example.com/"+strings.Repeat("a", URLBytesLimit-20), "todo-1")
	for _, want := range []string{"clawdline ops resolve ope_0123456789abcdef01234567 --todo todo-1 --title",
		"--needs-access --reason", "Do not paste the document's content", "resolve_stale"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(text) > 8<<10 {
		t.Fatalf("to-do is %d bytes", len(text))
	}
}
