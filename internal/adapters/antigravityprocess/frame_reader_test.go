package antigravityprocess

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func readFrames(t *testing.T, input string, max int) ([]string, error) {
	t.Helper()
	r := newFrameReader(strings.NewReader(input), max)
	var frames []string
	for {
		frame, err := r.next()
		if err != nil {
			if err == io.EOF {
				return frames, nil
			}
			return frames, err
		}
		frames = append(frames, string(frame))
	}
}

// The shape a Grok image Read sends: the picture inside a JSON-encoded
// rawOutput string and again in an image block.
func TestImageReadFrameDropsBothCopiesAndStaysJSON(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 3<<20))
	output, _ := json.Marshal(map[string]any{"ImageContent": map[string]string{"data": data, "mime_type": "image/jpeg"}})
	line, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "s", "update": map[string]any{
		"sessionUpdate": "tool_call_update", "toolCallId": "read-1", "status": "completed", "rawOutput": string(output),
		"content": []any{map[string]any{"type": "content", "content": map[string]string{"type": "image", "data": data, "mimeType": "image/jpeg"}}}}}})
	frames, err := readFrames(t, string(line)+"\n", 64<<10)
	if err != nil || len(frames) != 1 {
		t.Fatal(err, len(frames))
	}
	var got struct {
		Params struct {
			Update struct {
				Status, RawOutput string
				Content           []struct {
					Content struct{ Type, Data, MimeType string }
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(frames[0]), &got); err != nil {
		t.Fatalf("elided frame is not JSON: %v", err)
	}
	u := got.Params.Update
	marker := "[clawdline omitted " + itoa(len(data)) + " base64 characters]"
	var inner struct {
		ImageContent struct {
			Data     string
			MimeType string `json:"mime_type"`
		}
	}
	if err := json.Unmarshal([]byte(u.RawOutput), &inner); err != nil {
		t.Fatalf("rawOutput is no longer JSON: %v", err)
	}
	if u.Status != "completed" || inner.ImageContent.Data != marker || inner.ImageContent.MimeType != "image/jpeg" ||
		len(u.Content) != 1 || u.Content[0].Content.Data != marker || u.Content[0].Content.MimeType != "image/jpeg" {
		t.Fatalf("frame = %.400s", frames[0])
	}
	if len(frames[0]) > 1024 {
		t.Fatalf("elided frame still holds %d bytes", len(frames[0]))
	}
}

func TestFrameReaderKeepsOrdinaryText(t *testing.T) {
	short := strings.Repeat("A", MinElidedBase64Run-1)
	prose := strings.Repeat("word ", 4*MinElidedBase64Run)
	escaped := strings.Repeat(`ab\/`, MinElidedBase64Run)
	input := `{"a":"` + short + `"}` + "\n" + `{"a":"` + prose + `","b":"x\"y\\zA"}` + "\n" + `{"a":"` + escaped + `"}` + "\n" + `{"tail":1}`
	frames, err := readFrames(t, input, 1<<20)
	if err != nil || len(frames) != 4 {
		t.Fatal(err, len(frames))
	}
	if frames[0] != `{"a":"`+short+`"}` || frames[1] != `{"a":"`+prose+`","b":"x\"y\\zA"}` || frames[3] != `{"tail":1}` {
		t.Fatal("ordinary text changed")
	}
	if frames[2] != `{"a":"[clawdline omitted `+itoa(len(escaped))+` base64 characters]"}` {
		t.Fatalf("escaped-slash base64 kept: %.80s", frames[2])
	}
}

func TestFrameReaderStillRefusesLargeFrames(t *testing.T) {
	for name, input := range map[string]string{
		"unquoted run": strings.Repeat("x", 3*MinElidedBase64Run) + "\n",
		"long prose":   `{"a":"` + strings.Repeat("word ", MinElidedBase64Run) + `"}` + "\n",
	} {
		if _, err := readFrames(t, input, MinElidedBase64Run); err != errFrameTooLong {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
