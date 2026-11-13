package projectassistantacp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// permissionLifetimePeer is an in-process scripted wire peer, never a vendor CLI.
// The reply text is a wire receipt: the peer emits it only after checking the
// exact permission response followed by the native session/cancel notification.
func permissionLifetimePeer(input io.Reader, output io.Writer, mode string) error {
	send := func(f frame) {
		f.JSONRPC = "2.0"
		b, _ := json.Marshal(f)
		fmt.Fprintln(output, string(b))
	}
	offer := func() {
		send(frame{ID: raw("permission-rpc"), Method: "session/request_permission", Params: raw(map[string]any{
			"sessionId": "scripted-session", "toolCall": map[string]any{"toolCallId": "tool", "name": "Read", "rawInput": map[string]string{"file_path": "/project/readme"}},
			"options": []any{map[string]string{"optionId": "read-once", "kind": "allow_once", "name": "Read once"}, map[string]string{"optionId": "reject", "kind": "reject_once", "name": "Reject"}},
		})})
	}
	var promptID json.RawMessage
	cancelled := false
	cancelResponses := 0
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		var f frame
		if json.Unmarshal(scanner.Bytes(), &f) != nil {
			return fmt.Errorf("invalid host frame")
		}
		switch f.Method {
		case "initialize":
			send(frame{ID: f.ID, Result: raw(map[string]any{"protocolVersion": 1})})
		case "session/new":
			send(frame{ID: f.ID, Result: raw(map[string]string{"sessionId": "scripted-session"})})
		case "session/prompt":
			promptID = f.ID
			offer()
		case "session/cancel":
			var params struct {
				SessionID string `json:"sessionId"`
			}
			if !cancelled || len(f.ID) != 0 || json.Unmarshal(f.Params, &params) != nil || params.SessionID != "scripted-session" {
				return fmt.Errorf("session/cancel arrived without exact cancelled permission response")
			}
			if mode == "late" {
				// The same ID can be reused by a late native request. Cancellation
				// must seal admission before that frame reaches the consumer.
				offer()
			} else {
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "cancelled-wire-receipt"}}})})
				send(frame{ID: promptID, Result: raw(map[string]string{"stopReason": "cancelled"})})
			}
		case "":
			if string(f.ID) != `"permission-rpc"` {
				return fmt.Errorf("unexpected permission response ID")
			}
			var result struct {
				Outcome map[string]string `json:"outcome"`
			}
			if json.Unmarshal(f.Result, &result) != nil || len(result.Outcome) != 1 || result.Outcome["outcome"] != "cancelled" {
				return fmt.Errorf("permission response was not exactly cancelled")
			}
			cancelled = true
			cancelResponses++
			if mode == "late" && cancelResponses == 2 {
				send(frame{Method: "session/update", Params: raw(map[string]any{"sessionId": "scripted-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "late-permission-cancelled-wire-receipt"}}})})
			}
		}
	}
	return scanner.Err()
}

func openPermissionLifetimeClient(t *testing.T, mode string, sink func(context.Context, app.ProtocolEvent) error) *Client {
	t.Helper()
	input, stdin, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, output := io.Pipe()
	p := &Process{stdin: stdin, stdout: stdout, limits: testLimits(), write: make(chan struct{}, 1)}
	peerDone := make(chan error, 1)
	go func() {
		err := permissionLifetimePeer(input, output, mode)
		_ = output.CloseWithError(err)
		peerDone <- err
	}()
	c, err := NewClient(p, testLimits(), testFence(), sink, func([]byte) bool { return true })
	if err != nil {
		_ = stdin.Close()
		_ = input.Close()
		_ = stdout.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c.fail(refusal(pa.ExecutionEffectUnknown, "fixture_closed"))
		_ = stdin.Close()
		_ = stdout.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		defer cancel()
		if !waitOwnedDone(ctx, c.workerDone) {
			t.Error("client consumer did not stop")
		}
		select {
		case err := <-peerDone:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("scripted wire peer did not stop")
		}
		_ = input.Close()
	})
	if err := c.initialize(context.Background(), app.OpenRequest{Directory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	return c
}

func receivePermissionOffer(t *testing.T, offers <-chan pa.PermissionOffer) pa.PermissionOffer {
	t.Helper()
	select {
	case offer := <-offers:
		return offer
	case <-time.After(time.Second * 3):
		t.Fatal("permission offer did not arrive")
		return pa.PermissionOffer{}
	}
}

func TestACPPermissionCancelWritesCancelledAndRetiresAnswer(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 2)
	receipts := make(chan string, 2)
	c := openPermissionLifetimeClient(t, "normal", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		if e.Trace != nil {
			receipts <- e.Trace.Text
		}
		return nil
	})
	pending := sendTestPrompt(t, c)
	offer := receivePermissionOffer(t, offers)
	if err := c.Answer(context.Background(), offer.ID, "read-once"); pa.Code(err) != pa.NotFound {
		t.Fatalf("offer ID was confused with native RPC ID: %v", err)
	}
	if err := c.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); pa.Code(err) != pa.NotFound {
		t.Fatalf("cancelled permission remained answerable: %v", err)
	}
	if _, err := pending.Wait(context.Background()); pa.Code(err) != pa.ExecutionEffectUnknown {
		t.Fatalf("cancelled prompt promoted a successful reply: %v", err)
	}
	select {
	case receipt := <-receipts:
		if receipt != "cancelled-wire-receipt" {
			t.Fatalf("unexpected wire receipt: %q", receipt)
		}
	default:
		t.Fatal("peer did not verify cancelled response before session/cancel")
	}
	if c.Usage().PendingPermissions != 0 {
		t.Fatal("cancelled permission still counted as pending")
	}
}

func TestACPPermissionFenceAndOfferIdentityCannotBeReused(t *testing.T) {
	f := testFence()
	now := time.Now()
	offer := pa.PermissionOffer{ID: "offer", Fence: f, TurnID: "turn", NativeRequestID: `"permission-rpc"`, State: pa.PermissionPending, ExpiresAt: now.Add(time.Minute), Options: []pa.PermissionOption{{ID: "read-once", Kind: pa.AllowOnce, Allow: true, ReadOnly: true}}}
	answer := pa.PermissionAnswer{Fence: f, OfferID: offer.ID, TurnID: offer.TurnID, OptionID: "read-once"}
	permissionContext := pa.PermissionContext{Fence: f, ActiveTurnID: offer.TurnID, Now: now, Feature: pa.FeatureState{Enabled: true}, Source: pa.SourceStatus{MachineID: f.Key.MachineID, State: pa.SourceOnline, Readable: true, Writable: true, Fresh: true, Access: pa.Interactive}}
	if err := pa.CheckPermission(offer, answer, permissionContext); err != nil {
		t.Fatalf("valid control: %v", err)
	}
	for _, field := range []string{"generation", "binding", "conversation", "bot", "project", "offer", "turn", "native-id-as-offer"} {
		t.Run(field, func(t *testing.T) {
			stale := answer
			switch field {
			case "generation":
				stale.Fence.Generation++
			case "binding":
				stale.Fence.BindingID = "other"
			case "conversation":
				stale.Fence.ConversationID = "other"
			case "bot":
				stale.Fence.Key.BotID = "other"
			case "project":
				stale.Fence.Project.CanonicalID = "other"
			case "offer":
				stale.OfferID = "other"
			case "turn":
				stale.TurnID = "other"
			case "native-id-as-offer":
				stale.OfferID = offer.NativeRequestID
			}
			if err := pa.CheckPermission(offer, stale, permissionContext); err == nil {
				t.Fatal("foreign permission identity authorized")
			}
		})
	}
}

func TestACPPermissionCancelSealsLateNativeRequests(t *testing.T) {
	offers := make(chan pa.PermissionOffer, 2)
	receipts := make(chan string, 1)
	c := openPermissionLifetimeClient(t, "late", func(_ context.Context, e app.ProtocolEvent) error {
		if e.Permission != nil {
			offers <- *e.Permission
		}
		if e.Trace != nil {
			receipts <- e.Trace.Text
		}
		return nil
	})
	_ = sendTestPrompt(t, c)
	offer := receivePermissionOffer(t, offers)
	if err := c.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A normal second cancelled response is observable too: require the
	// peer's wire receipt rather than mistaking a quiet safe client for failure.
	select {
	case late := <-offers:
		if err := c.Answer(context.Background(), late.NativeRequestID, "read-once"); err == nil {
			t.Fatalf("late request revived cancelled native ID %s (original %s)", late.NativeRequestID, offer.NativeRequestID)
		}
	case text := <-receipts:
		if text != "late-permission-cancelled-wire-receipt" {
			t.Fatal("unexpected wire receipt", text)
		}
	case <-c.done:
	case <-time.After(time.Second * 3):
		t.Fatal("late permission was neither rejected nor handled")
	}
	if err := c.Answer(context.Background(), offer.NativeRequestID, "read-once"); pa.Code(err) != pa.NotFound {
		t.Fatal("retired native ID became answerable", err)
	}
	if c.Usage().PendingPermissions != 0 {
		t.Fatal("late permission retained an offer")
	}
}
