package transcript

import (
	"os"
	"strings"
	"testing"
)

func TestOlderPagesMeetWithoutMissingOrRepeatingARow(t *testing.T) {
	path := writeRecord(t,
		claudeRow("user", "first"),
		claudeRow("user", "second"),
		claudeRow("assistant", []m{{"type": "text", "text": "third"}, {"type": "text", "text": "fourth"}}),
		claudeRow("user", "fifth"),
	)
	var pages [][]string
	before := int64(0)
	for {
		page, err := ReadClaudeBefore(path, 2, before)
		if err != nil {
			t.Fatal(err)
		}
		var texts []string
		for _, entry := range page.Entries {
			texts = append(texts, entry.Text)
		}
		pages = append(pages, texts)
		if page.NextBefore == 0 {
			break
		}
		if page.NextBefore >= before && before != 0 {
			t.Fatalf("cursor did not move backward: %d -> %d", before, page.NextBefore)
		}
		before = page.NextBefore
	}
	got := []string{}
	for i := len(pages) - 1; i >= 0; i-- {
		got = append(got, pages[i]...)
	}
	if strings.Join(got, ",") != "first,second,third,fourth,fifth" {
		t.Fatalf("paged transcript: %q (pages %q)", got, pages)
	}
}

func TestOlderPageCanContinuePastTheReadWindow(t *testing.T) {
	path := writeRecord(t, claudeRow("user", "before window"))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Bookkeeping rows occupy more than one read window but yield no entries.
	row := []byte(`{"type":"progress","message":"` + strings.Repeat("x", 1024) + `"}` + "\n")
	for i := 0; i < 9000; i++ {
		if _, err := f.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := ReadClaudeBefore(path, 200, 0)
	if err != nil || first.NextBefore == 0 || len(first.Entries) != 0 {
		t.Fatalf("first window: %+v, %v", first, err)
	}
	older, err := ReadClaudeBefore(path, 200, first.NextBefore)
	if err != nil || len(older.Entries) != 1 || older.Entries[0].Text != "before window" {
		t.Fatalf("older window: %+v, %v", older, err)
	}
}

func TestCodexOlderPageUsesTheSameCursor(t *testing.T) {
	path := writeRecord(t,
		codexItem(m{"type": "UserMessage", "content": []m{{"type": "text", "text": "first"}}}),
		codexItem(m{"type": "UserMessage", "content": []m{{"type": "text", "text": "second"}}}),
	)
	latest, err := ReadCodexBefore(path, 1, 0)
	if err != nil || len(latest.Entries) != 1 || latest.Entries[0].Text != "second" || latest.NextBefore == 0 {
		t.Fatalf("latest: %+v, %v", latest, err)
	}
	older, err := ReadCodexBefore(path, 1, latest.NextBefore)
	if err != nil || len(older.Entries) != 1 || older.Entries[0].Text != "first" || older.NextBefore != 0 {
		t.Fatalf("older: %+v, %v", older, err)
	}
}
