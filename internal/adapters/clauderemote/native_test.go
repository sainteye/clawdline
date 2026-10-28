package clauderemote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/process"
	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Opt in to one exact locally owned test worker. Ordinary tests never read a
// real account. Sending additionally requires an unused caller-owned marker,
// created before the attempt; rerunning the command cannot submit twice.
func TestNativeRemoteControl(t *testing.T) {
	pid, _ := strconv.Atoi(os.Getenv("CLAWDLINE_RC_NATIVE_PID"))
	remote := os.Getenv("CLAWDLINE_RC_NATIVE_ID")
	if pid <= 0 || remote == "" {
		t.Skip("native RC test not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	identity := transcript.NewHost()
	identity.Refresh()
	p := process.New()
	p.RemoteControl = identity.RemoteControlSession
	inv, err := p.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var row session.Session
	for _, r := range inv.Sessions {
		if r.PID == pid && r.RemoteSessionID == remote {
			row = r
		}
	}
	if row.ID == "" || !process.VerifyRemoteControl(ctx, row) {
		t.Fatal("exact native worker was not discovered and verified")
	}
	c := New()
	before, err := c.Page(ctx, row, 200, 0, -1, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native worker discovered; persisted entries=%d latest_sequence=%d", len(before.Entries), before.NextAfter)
	if os.Getenv("CLAWDLINE_RC_NATIVE_SEND") != "1" {
		return
	}
	// The approved acceptance run may consume subscription quota only.
	authBefore, err := c.auth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	usageReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/api/oauth/usage", nil)
	usageReq.Header.Set("Authorization", "Bearer "+authBefore.token)
	usageReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
	usageResp, err := c.http.Do(usageReq)
	if err != nil {
		t.Fatal("cannot confirm extra usage is disabled; no send attempted")
	}
	var usage struct {
		Extra struct {
			Enabled *bool `json:"is_enabled"`
		} `json:"extra_usage"`
	}
	err = json.NewDecoder(usageResp.Body).Decode(&usage)
	usageResp.Body.Close()
	if err != nil || usageResp.StatusCode != 200 || usage.Extra.Enabled == nil || *usage.Extra.Enabled {
		t.Fatal("extra usage is not confirmed disabled; no send attempted")
	}
	marker := os.Getenv("CLAWDLINE_RC_NATIVE_ATTEMPT")
	if marker == "" {
		t.Fatal("an explicit unused attempt marker is required")
	}
	f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("attempt marker exists or cannot be created; no send attempted")
	}
	f.Close()
	text := "只回答 RC_TEST_OK_2，不要使用任何工具"
	a := app.Actions{Inventory: app.Inventory{Process: p, Identity: identity}, RemoteControl: c}
	var sent session.Session
	if os.Getenv("CLAWDLINE_RC_NATIVE_IMAGE") == "1" {
		text = "只回答這張合成圖片的主要顏色，不要使用任何工具。"
		picture := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				picture.Set(x, y, color.NRGBA{R: 255, A: 255})
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, picture); err != nil {
			t.Fatal(err)
		}
		sent, err = a.SendWithPictures(app.WithRemoteControlUserSubmission(ctx), row.ID, text, []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())})
	} else {
		sent, err = a.Send(app.WithRemoteControlUserSubmission(ctx), row.ID, text)
	}
	if err != nil {
		t.Fatal(err)
	}
	if sent.PID != row.PID || sent.RemoteSessionID != remote || !sent.ExpectedStart.Equal(row.ExpectedStart) {
		t.Fatal("send changed worker identity")
	}
	// One read stream, no reconnect/retry. Acceptance alone is not delivery.
	auth, err := c.auth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+"/v1/code/sessions/"+remote+"/events/stream?from_sequence_num="+strconv.FormatInt(before.NextAfter, 10), nil)
	req.Header.Set("Authorization", "Bearer "+auth.token)
	req.Header.Set("x-organization-uuid", auth.organization)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal("accepted, but worker observation could not be read")
	}
	defer resp.Body.Close()
	scan := bufio.NewScanner(io.LimitReader(resp.Body, ResponseBytesLimit+1))
	scan.Buffer(nil, ResponseBytesLimit)
	seenUser, seenFailure, seenResult, seenImage := false, false, false, false
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev event
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data:")), &ev) != nil {
			continue
		}
		var payload struct {
			Type         string  `json:"type"`
			Error        string  `json:"error"`
			IsError      bool    `json:"is_error"`
			TotalCostUSD float64 `json:"total_cost_usd"`
			DurationAPI  int64   `json:"duration_api_ms"`
			Message      struct {
				Content []struct {
					Text string `json:"text"`
					Type string `json:"type"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(ev.Payload, &payload) != nil {
			continue
		}
		for _, b := range payload.Message.Content {
			if ev.Source == "client" && payload.Type == "user" && b.Type == "image" {
				seenImage = true
			}
			if ev.Source == "client" && payload.Type == "user" && b.Text == text {
				seenUser = true
			}
		}
		if ev.Source == "worker" && payload.Type == "assistant" && payload.Error == "rate_limit" {
			seenFailure = true
		}
		if ev.Source == "worker" && payload.Type == "result" {
			seenResult = true
			t.Logf("worker result: is_error=%v total_cost_usd=%g duration_api_ms=%d", payload.IsError, payload.TotalCostUSD, payload.DurationAPI)
			if !payload.IsError && os.Getenv("CLAWDLINE_RC_EXPECT_QUOTA") == "1" {
				t.Fatal("expected a quota refusal, got model execution")
			}
			break
		}
	}
	if os.Getenv("CLAWDLINE_RC_NATIVE_IMAGE") == "1" && !seenImage {
		t.Fatal("native image block was not observed")
	}
	if !seenUser || !seenResult || os.Getenv("CLAWDLINE_RC_EXPECT_QUOTA") == "1" && !seenFailure {
		t.Fatalf("server accepted; worker evidence incomplete: user=%v quota=%v result=%v", seenUser, seenFailure, seenResult)
	}
	page, err := c.Page(ctx, row, 200, 0, -1, "")
	if err != nil {
		t.Fatal(err)
	}
	user, errorShown := false, false
	for _, e := range page.Entries {
		if e.Role == "user" && e.Text == text {
			user = true
		}
		if e.Role == "error" {
			errorShown = true
		}
	}
	if !user || os.Getenv("CLAWDLINE_RC_EXPECT_QUOTA") == "1" && !errorShown {
		t.Fatal("worker result is missing from the UI transcript projection")
	}
	t.Logf("one app send accepted; same worker consumed it; quota_failure=%v image_observed=%v; result observed; transcript projected", seenFailure, seenImage)
}

// Protocol-only acceptance: no user prompt, model call, or tool approval.
func TestNativeRCControls(t *testing.T) {
	pid, _ := strconv.Atoi(os.Getenv("CLAWDLINE_RC_CONTROL_PID"))
	remote := os.Getenv("CLAWDLINE_RC_CONTROL_ID")
	if pid <= 0 || remote == "" {
		t.Skip("native RC control test not requested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	identity := transcript.NewHost()
	identity.Refresh()
	p := process.New()
	p.RemoteControl = identity.RemoteControlSession
	inv, err := p.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var row session.Session
	for _, r := range inv.Sessions {
		if r.PID == pid && r.RemoteSessionID == remote {
			row = r
		}
	}
	if row.ID == "" || !process.VerifyRemoteControl(ctx, row) {
		t.Fatal("exact native control worker is unavailable")
	}
	c := New()
	menu, err := c.InspectRemote(ctx, row)
	if err != nil {
		t.Fatal(err)
	}
	if menu != nil {
		t.Fatal("test worker unexpectedly asks for a permission; it was not answered")
	}
	if err := c.InterruptRemote(ctx, row, nil); err != nil {
		t.Fatal(err)
	}
	t.Log("native initialize and idle interrupt both returned matching worker success responses; no user prompt sent")
	if os.Getenv("CLAWDLINE_RC_CONTROL_CLOSE") == "1" {
		marker := os.Getenv("CLAWDLINE_RC_CONTROL_ATTEMPT")
		if marker == "" {
			t.Fatal("close requires a one-use attempt marker")
		}
		f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("close marker exists or cannot be created")
		}
		f.Close()
		if err := c.CloseRemote(ctx, row, nil); err != nil {
			t.Fatal(err)
		}
		gone, err := process.RemoteControlGone(ctx, row)
		if err != nil || !gone {
			t.Fatal("archive was accepted but worker exit was not proved")
		}
		t.Log("native archive accepted and original worker process exit verified")
	}
}
