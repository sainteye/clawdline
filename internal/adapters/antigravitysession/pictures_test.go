//go:build darwin || linux

package antigravitysession

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 7, 7}

func lastPrompt(t *testing.T, dir string) []map[string]string {
	t.Helper()
	found, _ := filepath.Glob(filepath.Join(dir, "*-prompt"))
	if len(found) != 1 {
		t.Fatalf("prompt records: %q", found)
	}
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(readFile(t, found[0])), &blocks); err != nil {
		t.Fatal(err)
	}
	out := []map[string]string{}
	for _, b := range blocks {
		m := map[string]string{}
		for k, v := range b {
			if s, ok := v.(string); ok && s != "" {
				m[k] = s
			}
		}
		out = append(out, m)
	}
	return out
}

// A picture sent while a turn runs waits with its path, not its bytes, and
// is sent after the turn as a native image block beside its words.
func TestAPictureSentDuringATurnIsQueuedAndSentAsAnImageBlock(t *testing.T) {
	t.Setenv("CLW_IMAGE_PEER", "yes")
	h, row, dir := fixture(t)
	picture := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(picture, testPNG, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	if err := h.SendProtocolPictures(context.Background(), row, "what is this", []string{picture}, nil); err != nil {
		t.Fatalf("picture during a turn: %v", err)
	}
	c, _ := h.lookup(row)
	c.mu.Lock()
	queued := len(c.queued) == 1 && len(c.queuedPictures) == 1 && len(c.queuedPictures[0]) == 1
	c.mu.Unlock()
	if !queued {
		t.Fatal("the picture message was not queued with its path")
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	prompts := filepath.Join(dir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\n" })
	idleHost(t, h, row)
	blocks := lastPrompt(t, dir)
	if len(blocks) != 2 || blocks[0]["text"] != "what is this" || blocks[1]["type"] != "image" ||
		blocks[1]["mimeType"] != "image/png" || blocks[1]["data"] != base64.StdEncoding.EncodeToString(testPNG) {
		t.Fatalf("native prompt = %v", blocks)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	var counts []int64
	for _, e := range page.Entries {
		if e.Role == "user" {
			counts = append(counts, e.ImageCount)
		}
	}
	if len(counts) != 2 || counts[0] != 0 || counts[1] != 1 {
		t.Fatalf("user image counts = %v", counts)
	}
}

func TestPicturesForAnAgentWithoutImagesAreRefusedBeforeWriting(t *testing.T) {
	h, row, dir := fixture(t)
	picture := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(picture, testPNG, 0600); err != nil {
		t.Fatal(err)
	}
	err := h.SendProtocolPictures(context.Background(), row, "what is this", []string{picture}, nil)
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "images_unsupported" || !typed.BeforeWrite() {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
		t.Fatal("a picture prompt reached an agent without image support")
	}
	if strings.Join(userTurns(t, h, row), ",") != "" {
		t.Fatal("a refused picture message was recorded")
	}
}

// Review 2026-11-11: pictures that cannot fit one prompt were accepted into
// the queue and failed only when written. They are now refused at once.
func TestPicturesTooLargeForOnePromptAreRefusedBeforeQueueing(t *testing.T) {
	t.Setenv("CLW_IMAGE_PEER", "yes")
	h, row, _ := fixture(t)
	big := filepath.Join(t.TempDir(), "big.png")
	if err := os.WriteFile(big, append(append([]byte{}, testPNG...), make([]byte, 7<<20)...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	err := h.SendProtocolPictures(context.Background(), row, "too much", []string{big}, nil)
	var typed *Error
	if !errors.As(err, &typed) || !typed.PicturesTooLarge() || !typed.BeforeWrite() {
		t.Fatalf("err=%v", err)
	}
	c, _ := h.lookup(row)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queued) != 0 {
		t.Fatal("pictures too large for one prompt were queued")
	}
}

// Review 2026-11-11: one queued message whose picture failed made the whole
// queue fail. Only that message is kept as words; the next one still goes.
func TestAQueuedPictureThatFailsDropsOnlyItsOwnMessage(t *testing.T) {
	t.Setenv("CLW_IMAGE_PEER", "yes")
	h, row, dir := fixture(t)
	picture := filepath.Join(t.TempDir(), "gone.png")
	if err := os.WriteFile(picture, testPNG, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "first"); err != nil {
		t.Fatal(err)
	}
	if err := h.SendProtocolPictures(context.Background(), row, "with a picture", []string{picture}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Send(context.Background(), row, "third"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(picture); err != nil { // the drop cache pruned it
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	prompts := filepath.Join(dir, "prompts")
	waitFor(t, func() bool { return readFile(t, prompts) == "prompt\nprompt\n" })
	idleHost(t, h, row)
	if got := strings.Join(userTurns(t, h, row), ","); got != "first,third" {
		t.Fatalf("user turns = %q, want first,third", got)
	}
	page, err := h.Transcript(row, 100)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, e := range page.Entries {
		if e.Role == "error" && strings.HasPrefix(e.Text, QueuedNotSent) {
			kept = append(kept, strings.TrimPrefix(e.Text, QueuedNotSent))
			if e.ImageCount != 1 {
				t.Fatalf("kept message lost its picture count: %+v", e)
			}
		}
	}
	if strings.Join(kept, ",") != "with a picture" {
		t.Fatalf("kept as not sent = %q", kept)
	}
}
