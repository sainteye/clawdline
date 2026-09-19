package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
	"github.com/sainteye/clawdline-go/internal/transport/cloud"
)

// The whole Mac half of a Cloud write, in one process: the plaintext a hosted
// page seals (`web/console/src/cloud/relay-writer.ts` writes exactly these
// bodies, and its own tests assert them), decoded by the bridge, carried by
// the in-process router into this daemon's own route, down to the terminal.
// Only the gate is not in the way — its own refusals have their own tests —
// so the request arrives as the paired device the Cloud line signs in as.

func cloudBridge(s *Server) cloudops.Bridge {
	stamped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), accessKey{}, access{
			verdict: auth.Verdict{Allowed: true, Device: "cloud-line", Caps: auth.NewCaps(auth.Read, auth.Send)},
		}))
		s.sessionAction(w, r)
	})
	return cloudops.Bridge{MachineID: "mac-01", Router: cloud.Router{Handler: stamped},
		AllowCommands: func() bool { return true }}
}

func sealed(t *testing.T, seq uint64, body map[string]any) cloudops.Command {
	t.Helper()
	plaintext, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return cloudops.Command{Channel: "ctl/mac-01", Class: cloudops.ClassCtl, Sender: "web_viewer-01",
		Sequence: seq, Plaintext: plaintext}
}

func payloadError(t *testing.T, a cloudops.Answer) map[string]any {
	t.Helper()
	var out struct {
		Read  string         `json:"read"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(a.Payload, &out); err != nil {
		t.Fatalf("payload %s: %v", a.Payload, err)
	}
	return out.Error
}

// F1 end to end: the page answered the prompt for `rm -rf build`; by the time
// the envelope is opened on the Mac the prompt is for `rm -rf /`. The answer
// comes back as `menu_moved` on the channel the page is waiting on, and the
// terminal was not touched. The same answer against the question it names is
// typed and committed.
func TestACloudAnswerIsTypedOnlyAtTheQuestionItNames(t *testing.T) {
	p := waitingPane("%4", prompt("rm -rf /", 1))
	p.onKey = func(p *pane, b []byte) {
		if string(b) == "1" {
			p.show(prompt("rm -rf build", 1))
		}
	}
	bridge := cloudBridge(paneServer(t, p))
	expect := fingerprintOf(t, prompt("rm -rf build", 1))
	answer := bridge.Handle(context.Background(), sealed(t, 7, map[string]any{
		"type": "answer", "session": "%4", "request": "press-1", "answer": "1", "expect": expect}))
	if answer.Code != "menu_moved" || answer.Name != "action:press-1" || answer.Status != http.StatusConflict {
		t.Fatalf("answered %d/%q on %q", answer.Status, answer.Code, answer.Name)
	}
	if failure := payloadError(t, answer); failure["layer"] != "mac_route" {
		t.Fatalf("error %v", failure)
	}
	if got := p.done(); len(got) != 0 {
		t.Fatalf("typed %q at a question nobody saw", got)
	}

	p.show(prompt("rm -rf build", 1))
	answer = bridge.Handle(context.Background(), sealed(t, 8, map[string]any{
		"type": "answer", "session": "%4", "request": "press-2", "answer": "1", "expect": expect}))
	if !answer.OK() {
		t.Fatalf("answered %d/%q: %s", answer.Status, answer.Code, answer.Payload)
	}
	if got := p.done(); len(got) != 2 || got[0] != "key:1" || got[1] != "key:\r" {
		t.Fatalf("typed %q", got)
	}
}

// F2 end to end: the page's card sends its words under one request id for
// every attempt. The Mac typed them the first time and the answer was lost;
// "try again" is a second envelope — a new sequence — under the same id, and
// is answered with the first answer. The words were typed once.
func TestACloudSendTriedAgainUnderItsRequestIsTypedOnce(t *testing.T) {
	p := waitingPane("%4", "")
	bridge := cloudBridge(paneServer(t, p))
	body := map[string]any{"type": "send", "session": "%4", "request": "card-1",
		"text": "delete the build directory", "images": []any{}}
	first := bridge.Handle(context.Background(), sealed(t, 21, body))
	again := bridge.Handle(context.Background(), sealed(t, 22, body))
	if !first.OK() || !again.OK() || string(first.Payload) != string(again.Payload) {
		t.Fatalf("answers %d %s / %d %s", first.Status, first.Payload, again.Status, again.Payload)
	}
	if got := p.done(); len(got) != 1 {
		t.Fatalf("typed %q: the second attempt typed the words again", got)
	}
	// A menu answer and a close are carried the same way.
	p.show(prompt("rm -rf build", 1))
	press := map[string]any{"type": "answer", "session": "%4", "request": "press-9", "answer": "3",
		"expect": fingerprintOf(t, prompt("rm -rf build", 1))}
	bridge.Handle(context.Background(), sealed(t, 23, press))
	bridge.Handle(context.Background(), sealed(t, 24, press))
	end := map[string]any{"type": "end", "session": "%4", "request": "end-1", "accept_loss": false,
		"expected_closeability_version": ""}
	bridge.Handle(context.Background(), sealed(t, 25, end))
	bridge.Handle(context.Background(), sealed(t, 26, end))
	keys, closes := 0, 0
	for _, a := range p.done()[1:] {
		switch a {
		case "key:3":
			keys++
		case "close":
			closes++
		}
	}
	if keys != 1 || closes != 1 {
		t.Fatalf("acts %q: a retried answer or close was carried out twice", p.done())
	}
}
