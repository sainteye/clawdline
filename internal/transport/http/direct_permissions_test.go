package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/terminals"
	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/terminal"
)

func TestDirectPermissionManagementAndForgedGrants(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	id, token := f.device("phone", true)
	path := "/v1/auth/devices/" + id + "/permission"
	for _, caller := range []string{"", token} {
		if got := f.do(http.MethodPost, path, caller, `{"permission":"read-and-operate"}`); got.Code != 401 && got.Code != 403 {
			t.Fatalf("remote grant: %d %s", got.Code, got.Body)
		}
	}
	for _, permission := range []string{"read-only", "read-and-operate", "read-only"} {
		if got := f.do(http.MethodPost, path, f.local, `{"permission":"`+permission+`"}`); got.Code != 200 {
			t.Fatalf("grant: %d %s", got.Code, got.Body)
		}
		if got := f.do(http.MethodPost, "/v1/sessions/%254/send", token, `{"text":"synthetic"}`); (got.Code == 200) != (permission == "read-and-operate") {
			t.Fatalf("send %s: %d %s", permission, got.Code, got.Body)
		}
	}
	if got := f.do(http.MethodPost, path, token, `{"permission":"read-and-operate"}`); got.Code != 403 {
		t.Fatalf("reader self-promotion: %d %s", got.Code, got.Body)
	}
	if f.s.gate().auth.Verify(token).Caps.Has(auth.Send) {
		t.Fatal("reader promoted itself")
	}
	for _, path := range []string{"/v1/sessions", "/v1/board", "/v1/voice", "/v1/projects"} {
		if got := f.do(http.MethodPost, path, token, `{}`); got.Code != 403 {
			t.Fatalf("reader effect %s: %d %s", path, got.Code, got.Body)
		}
	}
	if got := f.do(http.MethodPost, path, f.local, `{"permission":"admin"}`); got.Code != 400 {
		t.Fatal(got.Code, got.Body)
	}
	if got := f.do(http.MethodPost, "/v1/auth/devices/missing/permission", f.local, `{"permission":"read-only"}`); got.Code != 404 {
		t.Fatal(got.Code, got.Body)
	}
}

func TestDirectPendingPairingScopeCannotBeForged(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	started, err := f.s.gate().auth.BeginPairing("synthetic")
	if err != nil {
		t.Fatal(err)
	}
	notices, cancel := f.s.gate().auth.Watch()
	defer cancel()
	notice := <-notices
	path := "/v1/auth/pairings/" + started.ID + "/permission"
	if got := f.do(http.MethodPost, path, "", `{"permission":"read-and-operate"}`); got.Code != 401 {
		t.Fatal(got.Code, got.Body)
	}
	if got := f.do(http.MethodPost, path, f.local, `{"permission":"read-only"}`); got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
	body, _ := json.Marshal(map[string]string{"pairing_id": started.ID, "code": notice.Code, "permission": "read-and-operate", "caps": "read,send"})
	got := f.do(http.MethodPost, "/v1/auth/pair/confirm", "", string(body))
	if got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
	var signed struct{ Token string }
	if err := json.Unmarshal(got.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	if v := f.s.gate().auth.Verify(signed.Token); !v.Allowed || v.Caps.Has(auth.Send) {
		t.Fatalf("forged scope: %+v", v)
	}
}

func TestDirectReadOnlyTerminalReadsRemainAndEffectsStop(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	term := f.open(f.local)
	id, token := f.device("phone", true)
	svc, err := f.s.terminalService()
	if err != nil {
		t.Fatal(err)
	}
	p := terminals.Principal{Device: id}
	if _, err := svc.SetControl(context.Background(), p, terminal.ID(term.ID), "tab", terminals.Acquire); err != nil {
		t.Fatal(err)
	}
	if err := f.s.gate().auth.SetPermission(id, true); err != nil {
		t.Fatal(err)
	}
	svc.Revalidate()
	if got := f.do(http.MethodGet, "/v1/terminals", token, ""); got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
	watch, err := svc.Watch(context.Background(), p, terminal.ID(term.ID), "reader")
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	if code, ok := terminal.CodeOf(svc.Resize(context.Background(), p, terminal.ID(term.ID), "tab", 1, 80, 24)); !ok || code != terminal.CodeForbidden {
		t.Fatalf("reader resize must fail authorization, not only lease checks: %q", code)
	}
	if code, ok := terminal.CodeOf(svc.Close(context.Background(), p, terminal.ID(term.ID), "tab", 1)); !ok || code != terminal.CodeForbidden {
		t.Fatalf("reader close must fail authorization, not only lease checks: %q", code)
	}
	if got := f.do(http.MethodPost, "/v1/terminals", token, `{"project_id":"prj_test","cols":80,"rows":24}`); got.Code != 403 {
		t.Fatal(got.Code, got.Body)
	}
	if _, got := f.control(term.ID, token, "tab", "acquire"); got.Code != 403 {
		t.Fatal(got.Code, got.Body)
	}
	if got := f.input(term.ID, token, "tab", 1, 1, "forbidden"); got.Code != 403 {
		t.Fatal(got.Code, got.Body)
	}
}

func TestDirectQueuedSessionSendRechecksRequestGrant(t *testing.T) {
	p := waitingPane("%4", "")
	s, h, _, _ := wholeServer(t, touchPane{p}, touchPane{p})
	id, token, err := s.gate().auth.AddDevice("phone", auth.NewCaps(auth.Read, auth.Send), false)
	if err != nil {
		t.Fatal(err)
	}
	lanes := app.TerminalLanes()
	release, err := lanes.Acquire(context.Background(), lane.TerminalKey(string(session.BackendTmux), "%4"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	admitted := lanes.Stats().Admitted
	done := make(chan int, 1)
	go func() {
		done <- call{method: http.MethodPost, path: "/v1/sessions/%254/send", body: `{"text":"never typed"}`, headers: map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}}.do(h).Code
	}()
	deadline := time.Now().Add(time.Second)
	for lanes.Stats().Admitted <= admitted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if lanes.Stats().Admitted <= admitted {
		t.Fatal("send did not queue")
	}
	if err := s.gate().auth.SetPermission(id, true); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case code := <-done:
		if code != 403 {
			t.Fatalf("queued send: %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("queued send did not finish")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, effect := range p.acts {
		if effect == "send:never typed" {
			t.Fatal("downgraded queued send reached the host")
		}
	}
}

func TestDirectBrowserPermissionAndLegacyBody(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	for _, body := range []string{`{}`, `{"permission":"read-only"}`} {
		got := f.do(http.MethodPost, "/v1/auth/devices/browser", f.local, body)
		if got.Code != 200 {
			t.Fatal(got.Code, got.Body)
		}
		var browser struct{ Token string }
		if err := json.Unmarshal(got.Body.Bytes(), &browser); err != nil {
			t.Fatal(err)
		}
		v := f.s.gate().auth.Verify(browser.Token)
		if !v.Allowed || v.Caps.Has(auth.Send) != (body == `{}`) {
			t.Fatalf("body %s: %+v", body, v)
		}
	}
}

// A read-only phone can manage its own notifications without gaining a
// machine-operation grant. Exercise the real router and gate; handler-only
// push tests already cover delivery and endpoint validation.
func TestDirectReadOnlyPushRemainsReadLevel(t *testing.T) {
	f := newTermFixture(t, newFakeTerms())
	id, token := f.device("synthetic reader", true)
	if err := f.s.gate().auth.SetPermission(id, true); err != nil {
		t.Fatal(err)
	}
	// An unsubscribed test has a typed response and performs no network send.
	if got := f.do(http.MethodPost, "/v1/push/test", token, `{}`); got.Code != http.StatusConflict || refusalCode(got) != "not_subscribed" {
		t.Fatalf("push test route: status=%d want=409 body=%s", got.Code, got.Body)
	}
	got := f.do(http.MethodPost, "/v1/push/subscribe", token, browserSubscription(t, "https://fcm.googleapis.com/fcm/send/synthetic-reader"))
	if got.Code != http.StatusOK {
		t.Fatalf("reader subscribe: %d %s", got.Code, got.Body)
	}
	var subscribed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &subscribed); err != nil || subscribed.ID == "" {
		t.Fatalf("subscribe result: %v %s", err, got.Body)
	}
	store, err := f.s.push()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := store.ForDevice(id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("reader subscription was not stored: count=%d err=%v", len(rows), err)
	}
	body, _ := json.Marshal(map[string]string{"id": subscribed.ID})
	if got := f.do(http.MethodPost, "/v1/push/unsubscribe", token, string(body)); got.Code != http.StatusOK {
		t.Fatalf("reader unsubscribe: %d %s", got.Code, got.Body)
	}
	if rows, err := store.ForDevice(id); err != nil || len(rows) != 0 {
		t.Fatalf("reader subscription was not removed: count=%d err=%v", len(rows), err)
	}
	for _, path := range []string{"/v1/push/subscribe", "/v1/push/unsubscribe", "/v1/push/test"} {
		if got := f.do(http.MethodPost, path, "", `{}`); got.Code != http.StatusUnauthorized {
			t.Fatalf("unpaired push %s: %d", path, got.Code)
		}
	}
	for _, path := range []string{"/v1/sessions", "/v1/board", "/v1/voice", "/v1/projects", "/v1/push/future-operation"} {
		if got := f.do(http.MethodPost, path, token, `{}`); got.Code != http.StatusForbidden {
			t.Fatalf("reader operation %s: %d", path, got.Code)
		}
	}
}
