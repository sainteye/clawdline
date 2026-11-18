package projectassistant

import (
	"strings"
	"testing"
)

func TestTopicNotesStayOutOfAutomaticFactCompaction(t *testing.T) {
	note := MemoryEntry{ID: "topic", Kind: TopicMemory, Title: "Release checklist", Summary: "Deployment steps", Text: "release: body-only details"}
	other := note
	other.ID = "another-topic"
	if _, changed := CompactMemoryEntries([]MemoryEntry{note, other}, 2); changed {
		t.Fatal("automatic compaction consumed named notes")
	}
	index := MemoryIndexEntry(note)
	if index.Text != note.Summary || strings.Contains(MemoryContextText(note), "body-only") {
		t.Fatal("body escaped into the topic index")
	}
	long := note
	long.Summary = strings.Repeat("界", MaxMemoryPreviewRunes+1)
	if got := len([]rune(MemoryIndexEntry(long).Text)); got != MaxMemoryPreviewRunes {
		t.Fatal("note index ignored the shared preview bound", got)
	}
	if err := ValidateMemoryContent(long); err != nil {
		t.Fatal("preview coalescing was incorrectly made a write refusal", err)
	}
}

func TestTopicNoteContentUsesExistingBoundsAndPurgedTombstones(t *testing.T) {
	valid := MemoryEntry{Kind: TopicMemory, Title: "Release notes", Summary: "A concise index", Text: "A detailed body"}
	for name, mutate := range map[string]func(*MemoryEntry){
		"unknown kind":      func(e *MemoryEntry) { e.Kind = "unsupported" },
		"legacy metadata":   func(e *MemoryEntry) { e.Kind = "" },
		"blank title":       func(e *MemoryEntry) { e.Title = " " },
		"title control":     func(e *MemoryEntry) { e.Title = "title\nline" },
		"long title":        func(e *MemoryEntry) { e.Title = strings.Repeat("x", MaxNameRunes+1) },
		"aggregate bytes":   func(e *MemoryEntry) { e.Text = strings.Repeat("x", MaxMessageBytes) },
		"forgotten content": func(e *MemoryEntry) { e.Forgotten = true },
	} {
		t.Run(name, func(t *testing.T) {
			entry := valid
			mutate(&entry)
			if err := ValidateMemoryContent(entry); err == nil {
				t.Fatal("invalid topic content was accepted")
			}
		})
	}
	valid.Forgotten = true
	valid.Title, valid.Summary, valid.Text = "", "", ""
	if err := ValidateMemoryContent(valid); err != nil {
		t.Fatal("purged note tombstone refused", err)
	}
}
