package cloudops

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func projectAssistantViewBody() body {
	return body{"type": "project-assistants.view", "session": MachineReplySession, "request": "view-request",
		"key": map[string]any{"machine_id": "source & 一", "bot_id": "Bot ?#🌱"}, "conversation_id": "chat?&#",
		"mode": "snapshot", "source_epoch": "", "cursor": "", "after_seq": json.Number("0"), "page_size": json.Number("100")}
}

func TestProjectAssistantViewCloudPreservesCompleteEncodedRequest(t *testing.T) {
	for _, mode := range []string{"snapshot", "history", "events", "permissions", "traces", "detail"} {
		t.Run(mode, func(t *testing.T) {
			b := projectAssistantViewBody()
			b["mode"] = mode
			calls := 0
			bridge := open(projectAssistantRouter(func(_ context.Context, r LocalRequest) (LocalResponse, error) {
				calls++
				u, err := url.Parse(r.Path)
				if err != nil {
					t.Fatal(err)
				}
				if r.Method != "GET" || len(r.Body) != 0 || u.EscapedPath() != "/v1/project-assistants/"+url.PathEscape("Bot ?#🌱")+"/view" {
					t.Fatalf("bad path/body: %+v", r)
				}
				want := map[string]string{"machine_id": "source & 一", "conversation_id": "chat?&#", "mode": mode, "source_epoch": "", "cursor": "", "after_seq": "0", "page_size": "100"}
				if !reflect.DeepEqual(r.Query, want) {
					t.Fatalf("dropped empty/zero/encoded fields: %v", r.Query)
				}
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1","items":[]}`)}, nil
			}))
			bridge.AllowCommands = func() bool { return false }
			answer := bridge.Handle(context.Background(), request(t, ClassCtl, b))
			if !answer.OK() || calls != 1 {
				t.Fatalf("read required content-write switch: %+v calls=%d", answer, calls)
			}
		})
	}
}

func TestProjectAssistantViewCloudClosedRequiredAndTypedFailures(t *testing.T) {
	base := projectAssistantViewBody()
	mutations := []body{}
	for _, key := range []string{"key", "conversation_id", "mode", "source_epoch", "cursor", "after_seq", "page_size"} {
		b := cloneBody(base)
		delete(b, key)
		mutations = append(mutations, b)
		b = cloneBody(base)
		b[key] = nil
		mutations = append(mutations, b)
	}
	for _, value := range []any{json.Number("1e0"), json.Number("1.5"), json.Number("-1"), "0"} {
		b := cloneBody(base)
		b["after_seq"] = value
		mutations = append(mutations, b)
	}
	for _, value := range []any{json.Number("0"), json.Number("257"), json.Number("1.0"), "1"} {
		b := cloneBody(base)
		b["page_size"] = value
		mutations = append(mutations, b)
	}
	for _, key := range []string{"principal", "authorization_binding", "byte_limit", "cursor_byte_limit"} {
		b := cloneBody(base)
		b[key] = "forged"
		mutations = append(mutations, b)
	}
	b := cloneBody(base)
	b["key"].(map[string]any)["account"] = "forged"
	mutations = append(mutations, b)
	b = cloneBody(base)
	b["cursor"] = strings.Repeat("x", 4097)
	mutations = append(mutations, b)
	b = cloneBody(base)
	b["mode"] = "export"
	mutations = append(mutations, b)
	for i, b := range mutations {
		calls := 0
		bridge := open(projectAssistantRouter(func(context.Context, LocalRequest) (LocalResponse, error) {
			calls++
			return LocalResponse{Status: 200, Body: []byte(`{}`)}, nil
		}))
		if a := bridge.Handle(context.Background(), request(t, ClassCtl, b)); a.OK() || calls != 0 {
			t.Fatalf("mutation %d reached source", i)
		}
	}
	for _, code := range []pa.ErrorCode{pa.CursorExpired, pa.StaleGeneration, pa.ScopeDenied, pa.CapacityFull, pa.Revoked} {
		bridge := open(projectAssistantRouter(func(context.Context, LocalRequest) (LocalResponse, error) {
			return LocalResponse{Status: 429, Body: []byte(`{"contract_version":"project-assistants.v1","error":{"code":"` + string(code) + `","field":"view","retry_after_seconds":0}}`)}, nil
		}))
		a := bridge.Handle(context.Background(), request(t, ClassCtl, base))
		if a.OK() || !strings.Contains(string(a.Payload), `"code":"`+string(code)+`"`) || !strings.Contains(string(a.Payload), `"field":"view"`) {
			t.Fatalf("lost typed failure %s: %s", code, a.Payload)
		}
	}
}
