package projectassistant

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

const TopicMemory = "note"

// Notes share the existing entry count, message bytes and name limits. Their
// index uses the registered memory-preview budget; it never includes the body.
func ValidateMemoryContent(entry MemoryEntry) error {
	if entry.Kind == "" {
		if entry.Title != "" || entry.Summary != "" {
			return fail(Invalid, "memory_kind")
		}
		return nil // Preserve legacy short-fact validation and archive semantics.
	}
	if entry.Kind != TopicMemory {
		return fail(Invalid, "memory_kind")
	}
	if entry.Forgotten {
		if entry.Text != "" || entry.Title != "" || entry.Summary != "" {
			return fail(ContextStale, "forgotten_note")
		}
		return nil
	}
	if !utf8.ValidString(entry.Title) || strings.TrimSpace(entry.Title) == "" || utf8.RuneCountInString(entry.Title) > MaxNameRunes || strings.ContainsFunc(entry.Title, unicode.IsControl) {
		return fail(Invalid, "memory_title")
	}
	if err := ValidateMessage(entry.Summary); err != nil {
		return err
	}
	if len(entry.Title)+len(entry.Summary)+len(entry.Text) > MaxMessageBytes {
		return fail(CapacityFull, "memory_note_bytes")
	}
	return ValidateMessage(entry.Text)
}

func MemoryPreview(text string) string {
	runes := []rune(text)
	if len(runes) > MaxMemoryPreviewRunes {
		return string(runes[:MaxMemoryPreviewRunes])
	}
	return text
}

// MemoryIndexEntry is a detached model-facing projection, not an editable body.
func MemoryIndexEntry(entry MemoryEntry) MemoryEntry {
	if entry.Kind == TopicMemory {
		entry.Summary = MemoryPreview(entry.Summary)
		entry.Text = entry.Summary
	}
	return entry
}

func MemoryContextText(entry MemoryEntry) string {
	if entry.Kind != TopicMemory {
		return entry.Text
	}
	index, _ := json.Marshal(struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}{entry.ID, entry.Title, MemoryPreview(entry.Summary)})
	return "Topic note index: " + string(index)
}
