package cloudops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

var groupCloudWords = map[string]string{
	"groups": "GET", "group-history": "GET", "group-create": "POST", "group-edit": "PATCH", "group-delete": "DELETE", "group-stop": "POST", "group-send": "POST", "group-react": "POST", "team-plans": "GET", "team-answer": "POST", "team-settings": "POST",
}

func projectAssistantGroupOperationCases() []operationCase {
	rows, err := cloudvocab.Operations()
	if err != nil {
		panic(err)
	}
	out := []operationCase{}
	for _, row := range rows {
		word := strings.TrimPrefix(row.Op, "project-assistants.")
		method, ok := groupCloudWords[word]
		if !ok {
			continue
		}
		raw, _ := json.Marshal(row.Example)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		path := "/v1/project-assistants/rooms/groups"
		q := map[string]string{}
		switch word {
		case "groups":
			q["machine_id"] = "source-one"
		case "group-history":
			path += "/room-one"
			q = map[string]string{"machine_id": "source-one", "before_seq": "0", "page_size": "100"}
		case "group-edit", "group-delete":
			path += "/room-one"
		case "group-stop":
			path += "/room-one/stop"
		case "group-send":
			path += "/room-one/messages"
		case "group-react":
			path += "/room-one/reactions"
		case "team-plans":
			path = "/v1/project-assistants/manager-one/team-plans"
			q["machine_id"] = "source-one"
		case "team-answer":
			path = "/v1/project-assistants/manager-one/team-answer"
		case "team-settings":
			path = "/v1/project-assistants/manager-one/team-settings"
		}
		c := operationCase{word: row.Op, method: method, path: path, body: fields, query: q, session: MachineReplySession}
		c.answers = &router{body: `{"contract_version":"project-assistants.v1","ok":true}`}
		if method != "GET" {
			local := map[string]any{}
			for k, v := range fields {
				if k != "request" && k != "session" {
					local[k] = v
				}
			}
			raw, _ := json.Marshal(local)
			c.body2 = string(raw)
			c.name = "action:" + fields["request"].(string)
		} else {
			c.name = "read:" + fields["request"].(string)
		}
		fields["type"] = row.Op
		out = append(out, c)
	}
	return out
}

func TestGroupCloudWordsCarryTheSameRequestAndKeepTheWriteCeiling(t *testing.T) {
	rows, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		word := strings.TrimPrefix(row.Op, "project-assistants.")
		method, ok := groupCloudWords[word]
		if !ok {
			continue
		}
		t.Run(word, func(t *testing.T) {
			b := projectAssistantExample(t, row)
			calls := 0
			bridge := open(projectAssistantRouter(func(_ context.Context, r LocalRequest) (LocalResponse, error) {
				calls++
				if r.Method != method {
					t.Fatal(r.Method, method)
				}
				return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
			}))
			answer := bridge.Handle(t.Context(), request(t, ClassCtl, b))
			if !answer.OK() || calls != 1 {
				t.Fatal(answer, calls)
			}
			bridge.AllowCommands = func() bool { return false }
			answer = bridge.Handle(t.Context(), request(t, ClassCtl, b))
			if (method == "GET") != answer.OK() {
				t.Fatal("incorrect source write ceiling", answer)
			}
		})
	}
}

func TestGroupCloudDecoderRefusesCrossMachineMembersAndPartialApproval(t *testing.T) {
	rows, _ := cloudvocab.Operations()
	for _, row := range rows {
		if row.Op != "project-assistants.group-create" && row.Op != "project-assistants.team-answer" {
			continue
		}
		b := projectAssistantExample(t, row)
		calls := 0
		bridge := open(projectAssistantRouter(func(context.Context, LocalRequest) (LocalResponse, error) {
			calls++
			return LocalResponse{Status: 200, Body: []byte(`{"contract_version":"project-assistants.v1"}`)}, nil
		}))
		if strings.HasSuffix(row.Op, "group-create") {
			b["settings"].(map[string]any)["members"].([]any)[0].(map[string]any)["machine_id"] = "other"
		} else {
			delete(b, "allow")
		}
		if answer := bridge.Handle(t.Context(), request(t, ClassCtl, b)); answer.OK() || calls != 0 {
			t.Fatal(answer, calls)
		}
	}
}
