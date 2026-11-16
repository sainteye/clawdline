package projectassistantacp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type mcpBrowserTools struct {
	mcpTestTools
	status  func(context.Context) (app.BrowserStatus, error)
	command func(context.Context, app.BrowserCommand) (app.BrowserResult, error)
}

func (t mcpBrowserTools) BrowserStatus(ctx context.Context) (app.BrowserStatus, error) {
	return t.status(ctx)
}
func (t mcpBrowserTools) BrowserCommand(ctx context.Context, q app.BrowserCommand) (app.BrowserResult, error) {
	return t.command(ctx, q)
}

type mcpReply struct {
	Result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		} `json:"content"`
	} `json:"result"`
	Error *rpcError `json:"error"`
}

func (f *mcpFixture) call(t *testing.T, name string, args any) mcpReply {
	t.Helper()
	code, b := f.post(t, mcpCall(name, args), nil)
	var r mcpReply
	if code != 200 || json.Unmarshal(b, &r) != nil || r.Error != nil {
		t.Fatalf("%s: %d %s", name, code, b)
	}
	return r
}

func testJPEG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestOwnedMCPBrowserToolsCarryValuesImagesAndTypedRefusals(t *testing.T) {
	shot := testJPEG(t)
	var seen []app.BrowserCommand
	f := newMCPFixture(t, mcpBrowserTools{
		status: func(context.Context) (app.BrowserStatus, error) {
			return app.BrowserStatus{Mode: "isolated", Connected: true, Tabs: []app.BrowserTab{{Tab: 4, Mode: "isolated", Current: true}}}, nil
		},
		command: func(_ context.Context, q app.BrowserCommand) (app.BrowserResult, error) {
			seen = append(seen, q)
			switch q.Op {
			case "screenshot":
				return app.BrowserResult{Value: json.RawMessage(`{"data":"` + shot + `","url":"http://127.0.0.1/page"}`)}, nil
			case "click":
				return app.BrowserResult{}, &app.BrowserFailure{Code: "browser_person_controlling"}
			case "type":
				return app.BrowserResult{}, &app.BrowserFailure{Code: "browser_page_error", Message: "no element labelled Name"}
			case "key":
				return app.BrowserResult{}, &pa.Failure{Code: pa.StaleGeneration, Field: "native_work_turn"}
			case "scroll":
				return app.BrowserResult{Value: json.RawMessage(`"` + strings.Repeat("x", testLimits().FrameBytes) + `"`)}, nil
			}
			return app.BrowserResult{Value: json.RawMessage(`{"text":"Hello"}`)}, nil
		},
	})
	f.initialize(t)
	code, list := f.post(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if code != 200 || !bytes.Contains(list, []byte(`"browser_command"`)) || !bytes.Contains(list, []byte(`"browser_status"`)) {
		t.Fatalf("tools/list: %d %s", code, list)
	}
	status := f.call(t, "browser_status", nil)
	if status.Result.IsError || !strings.Contains(status.Result.Content[0].Text, `"tab":4`) {
		t.Fatalf("status: %+v", status)
	}
	read := f.call(t, "browser_command", map[string]any{"op": "read"})
	if read.Result.IsError || read.Result.Content[0].Text != `{"op":"read","value":{"text":"Hello"}}` {
		t.Fatalf("read: %+v", read)
	}
	picture := f.call(t, "browser_command", map[string]any{"op": "screenshot", "args": map[string]any{"tab": 4}})
	if picture.Result.IsError || len(picture.Result.Content) != 2 || picture.Result.Content[1].Type != "image" || picture.Result.Content[1].MimeType != "image/jpeg" {
		t.Fatalf("screenshot: %+v", picture)
	}
	raw, err := base64.StdEncoding.DecodeString(picture.Result.Content[1].Data)
	if err != nil {
		t.Fatal(err)
	}
	if img, err := jpeg.Decode(bytes.NewReader(raw)); err != nil || img.Bounds().Dx() != 8 {
		t.Fatalf("the image does not decode: %v", err)
	}
	for op, want := range map[string]string{
		"click":  `{"code":"browser_person_controlling"}`,
		"type":   `{"code":"browser_page_error","message":"no element labelled Name"}`,
		"key":    `{"code":"pa_stale_generation"}`,
		"scroll": `{"code":"browser_result_too_large"}`,
	} {
		r := f.call(t, "browser_command", map[string]any{"op": op})
		if !r.Result.IsError || r.Result.Content[0].Text != want {
			t.Fatalf("%s: %+v, want %s", op, r, want)
		}
	}
	// No owner, Bot, fence or explicit choice can be passed.
	for _, args := range []map[string]any{{"op": "read", "owner": "bot-other"}, {"op": "read", "explicit": true}, {"op": "read", "bot_id": "x"}} {
		r := f.call(t, "browser_command", args)
		if !r.Result.IsError || r.Result.Content[0].Text != `{"code":"pa_invalid"}` {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	if len(seen) != 6 || seen[1].Op != "screenshot" || string(seen[1].Args) != `{"tab":4}` {
		t.Fatalf("commands = %+v", seen)
	}
}

// An adapter without browser tools answers unsupported, never a fake success.
func TestOwnedMCPWithoutBrowserToolsIsUnsupported(t *testing.T) {
	f := newMCPFixture(t, mcpTestTools{})
	f.initialize(t)
	r := f.call(t, "browser_command", map[string]any{"op": "read"})
	if !r.Result.IsError || r.Result.Content[0].Text != `{"code":"pa_unsupported"}` {
		t.Fatalf("%+v", r)
	}
}
