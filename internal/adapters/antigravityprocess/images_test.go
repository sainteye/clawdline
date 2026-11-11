//go:build darwin || linux

package antigravityprocess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}

func promptFrames(t *testing.T, dir string) [][]json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "wire"))
	if err != nil {
		t.Fatal(err)
	}
	var prompts [][]json.RawMessage
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var frame struct {
			Method string
			Params struct{ Prompt []json.RawMessage }
		}
		if json.Unmarshal([]byte(line), &frame) != nil {
			t.Fatal("invalid peer wire")
		}
		if frame.Method == "session/prompt" {
			prompts = append(prompts, frame.Params.Prompt)
		}
	}
	return prompts
}

func TestPicturesAreImageBlocksAfterTheText(t *testing.T) {
	c, dir := fake(t, "images", nil)
	ready(t, c)
	if !c.AcceptsImages() {
		t.Fatal("advertised image capability not negotiated")
	}
	images := []PromptImage{{MimeType: "image/png", Data: pngBytes}, {MimeType: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff, 9}}}
	if _, err := c.PromptImagesChecked(context.Background(), "what is this", "", "", images, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PromptImagesChecked(context.Background(), "", "", "", images[:1], nil, nil); err != nil {
		t.Fatal(err)
	}
	prompts := promptFrames(t, dir)
	if len(prompts) != 2 || len(prompts[0]) != 3 || len(prompts[1]) != 1 {
		t.Fatalf("prompts: %d", len(prompts))
	}
	if string(prompts[0][0]) != `{"text":"what is this","type":"text"}` {
		t.Fatal(string(prompts[0][0]))
	}
	want := `{"data":"` + base64.StdEncoding.EncodeToString(pngBytes) + `","mimeType":"image/png","type":"image"}`
	if string(prompts[0][1]) != want || string(prompts[1][0]) != want {
		t.Fatalf("image block: %s", prompts[0][1])
	}
	if !strings.Contains(string(prompts[0][2]), `"mimeType":"image/jpeg"`) {
		t.Fatal(string(prompts[0][2]))
	}
}

func TestPicturesWithoutTheCapabilityWriteNothing(t *testing.T) {
	c, dir := fake(t, "happy", nil)
	ready(t, c)
	_, err := c.PromptImagesChecked(context.Background(), "what is this", "", "", []PromptImage{{MimeType: "image/png", Data: pngBytes}}, nil, nil)
	requireError(t, err, "images_unsupported", NotWritten)
	if len(promptFrames(t, dir)) != 0 {
		t.Fatal("pictures were written to an agent that did not advertise images")
	}
}
