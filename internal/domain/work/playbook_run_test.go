package work

import "testing"

func TestPlaybookRunTitleReadsWhatTheConsoleWrites(t *testing.T) {
	const id = "f1000001-0000-4000-8000-000000000001"
	for _, c := range []struct {
		text  string
		title string
		ok    bool
	}{
		// playbookTodoText(id, "重新設計", "message")
		{"message\n\n[Clawdline-Playbook " + id + " %E9%87%8D%E6%96%B0%E8%A8%AD%E8%A8%88]", "重新設計", true},
		{"a\n\n[Clawdline-Playbook " + id + " x]\n\n[Clawdline-Playbook " + id + " Review%20it]", "Review it", true},
		{"plain to-do", "", false},
		{"message\n\n[Clawdline-Playbook " + id + " ]", "", false},
		{"message\n\n[Clawdline-Playbook not-a-uuid Title]", "", false},
		{"message\n\n[Clawdline-Playbook " + id + " %E9%87]", "", false},
		{"message\n\n[Clawdline-Playbook " + id + " Title] trailing", "", false},
		{"message\n\n[Clawdline-Playbook ]", "", false},
	} {
		title, ok := PlaybookRunTitle(c.text)
		if title != c.title || ok != c.ok {
			t.Errorf("PlaybookRunTitle(%q) = %q, %v; want %q, %v", c.text, title, ok, c.title, c.ok)
		}
	}
}
