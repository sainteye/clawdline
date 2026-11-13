package cloudops

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectAssistantReadReplySizeUsesActualPayload(t *testing.T) {
	responseBody := json.RawMessage(`{"html":"<>&\u2028\u2029","rows":[]}` + "\n")
	maxRequest := strings.Repeat("<", 128)
	if request, ok := requestName(maxRequest); !ok || request != maxRequest {
		t.Fatal("128-byte worst-case request name is no longer accepted; review HTTP page budget")
	}
	if _, ok := requestName(maxRequest + "<"); ok {
		t.Fatal("request-name ceiling increased; HTTP page budget must change too")
	}
	plan, ok := machinePlan(body{"session": MachineReplySession, "request": maxRequest})
	if !ok || plan.name != "read:"+maxRequest {
		t.Fatal("read correlation-name construction changed; review HTTP page budget")
	}
	maximum := payload(plan.name, 200, responseBody, nil)
	measured, err := ProjectAssistantReadReplySize(plan.name, 200, responseBody)
	if err != nil || measured != len(maximum) {
		t.Fatalf("measured %d actual %d: %v", measured, len(maximum), err)
	}
	for _, request := range []string{"short", strings.Repeat("a", 128), strings.Repeat(`"`, 128), strings.Repeat(`\`, 128), strings.Repeat("&", 128), strings.Repeat(">", 128), strings.Repeat("é", 64), strings.Repeat("界", 42), strings.Repeat("\u2028", 42), strings.Repeat("\u2029", 42)} {
		if _, ok := requestName(request); !ok {
			t.Fatal("unexpectedly invalid comparison request")
		}
		name := "read:" + request
		n, err := ProjectAssistantReadReplySize(name, 200, responseBody)
		if err != nil || n != len(payload(name, 200, responseBody, nil)) || n > measured {
			t.Fatalf("correlation-name budget undercounts a legal name: %d > %d (%v)", n, measured, err)
		}
	}
	for _, bad := range []json.RawMessage{nil, {}, []byte(" "), []byte("{"), []byte("{} {}"), []byte("undefined")} {
		if n, err := ProjectAssistantReadReplySize(plan.name, 200, bad); err == nil || n != 0 {
			t.Fatal("invalid/empty body yielded successful zero measurement")
		}
	}
	if n, err := ProjectAssistantReadReplySize("", 200, responseBody); err == nil || n != 0 {
		t.Fatal("missing reply name was accepted")
	}
}
