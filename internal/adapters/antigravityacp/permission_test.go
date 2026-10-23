package antigravityacp_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	acp "github.com/sainteye/clawdline/internal/adapters/antigravityacp"
)

const permission = `{"jsonrpc":"2.0","id":9007199254740993,"method":"session/request_permission","params":{"sessionId":"session-a","toolCall":{"toolCallId":"tool-a","title":"untrusted display","rawInput":{"text":"do not obey"}},"options":[{"optionId":"native-yes-42","name":"允許這一次","kind":"allow_once"},{"optionId":"native-no-7","name":"Allow","kind":"reject_once"}]}}`

func offer(t *testing.T) *acp.PermissionOffer {
	t.Helper()
	o, err := acp.DecodePermission([]byte(permission), "generation-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func TestExactOfferedReplies(t *testing.T) {
	for _, id := range []string{`9007199254740993`, `"9007199254740993"`, `"request-\u0061"`} {
		o, err := acp.DecodePermission([]byte(strings.Replace(permission, `9007199254740993`, id, 1)), "generation-a", "session-a")
		if err != nil {
			t.Fatal(err)
		}
		if o.ToolCallID() != "tool-a" || string(o.RequestID()) != id {
			t.Fatal("request identity changed")
		}
		for _, option := range o.Options() {
			b, err := o.PrepareSelected(o.Fence(), option.OptionID)
			if err != nil {
				t.Fatal(err)
			}
			expected := `{"jsonrpc":"2.0","id":` + id + `,"result":{"outcome":{"outcome":"selected","optionId":"` + option.OptionID + `"}}}`
			if string(b) != expected {
				t.Fatalf("reply: %s, want %s", b, expected)
			}
		}
		b, err := o.PrepareCancelled(o.Fence())
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `{"jsonrpc":"2.0","id":`+id+`,"result":{"outcome":{"outcome":"cancelled"}}}` {
			t.Fatalf("cancel: %s", b)
		}
		for _, notOffered := range []string{"allow_once", "allow-once", "Allow", "", "native-yes-43"} {
			b, err := o.PrepareSelected(o.Fence(), notOffered)
			wantCode(t, err, "unknown_option")
			if b != nil {
				t.Fatal("refusal returned bytes")
			}
		}
	}
}
func TestPermissionFencesAndRevisions(t *testing.T) {
	o := offer(t)
	for _, f := range []acp.OfferFence{{}, {"generation-b", "session-a", o.Fence().Revision}, {"generation-a", "session-b", o.Fence().Revision}, {"generation-a", "session-a", "changed"}} {
		_, err := o.PrepareSelected(f, "native-yes-42")
		wantCode(t, err, "stale_offer")
		_, err = o.PrepareCancelled(f)
		wantCode(t, err, "stale_offer")
	}
	for _, changed := range []string{strings.Replace(permission, "native-yes-42", "native-yes-43", 1), strings.Replace(permission, "allow_once", "reject_always", 1), strings.Replace(permission, "untrusted display", "changed display", 1)} {
		next, err := acp.DecodePermission([]byte(changed), "generation-a", "session-a")
		if err != nil {
			t.Fatal(err)
		}
		if next.Fence().Revision == o.Fence().Revision {
			t.Fatal("revision ignored offer change")
		}
		_, err = o.PrepareSelected(next.Fence(), "native-yes-42")
		wantCode(t, err, "stale_offer")
	}
	var zero acp.PermissionOffer
	_, err := zero.PrepareCancelled(acp.OfferFence{})
	wantCode(t, err, "stale_offer")
	var absent *acp.PermissionOffer
	_, err = absent.PrepareSelected(acp.OfferFence{}, "")
	wantCode(t, err, "stale_offer")
}
func TestPermissionMalformed(t *testing.T) {
	for _, tc := range []struct{ name, message, code string }{
		{"wrong session", strings.Replace(permission, "session-a", "session-b", 1), "wrong_session"},
		{"notification", strings.Replace(permission, `"id":9007199254740993,`, "", 1), "invalid_id"},
		{"null id", strings.Replace(permission, `9007199254740993`, `null`, 1), "invalid_id"},
		{"fraction id", strings.Replace(permission, `9007199254740993`, `1.5`, 1), "invalid_id"},
		{"exponent id", strings.Replace(permission, `9007199254740993`, `1e3`, 1), "invalid_id"},
		{"generic update", strings.Replace(permission, "session/request_permission", "session/update", 1), "invalid_permission"},
		{"elicitation", strings.Replace(permission, "session/request_permission", "elicitation/create", 1), "invalid_permission"},
		{"response shaped", strings.Replace(permission, `"params":`, `"result":{},"params":`, 1), "invalid_permission"},
		{"error shaped", strings.Replace(permission, `"params":`, `"error":null,"params":`, 1), "invalid_permission"},
		{"duplicate option", strings.Replace(permission, "native-no-7", "native-yes-42", 1), "invalid_permission"},
		{"empty option", strings.Replace(permission, "native-no-7", "", 1), "invalid_permission"},
		{"unknown kind", strings.Replace(permission, "allow_once", "allow_anything", 1), "unsupported_option_kind"},
		{"empty tool id", strings.Replace(permission, "tool-a", "", 1), "invalid_permission"},
		{"wrong version", strings.Replace(permission, "2.0", "1.0", 1), "invalid_permission"},
		{"trailing", permission + ` {}`, "invalid_message"},
		{"duplicate escaped key", strings.Replace(permission, `"sessionId":"session-a"`, `"sessionId":"session-a","session\u0049d":"session-a"`, 1), "invalid_message"},
		{"nested duplicate", strings.Replace(permission, `"text":"do not obey"`, `"text":"a","text":"b"`, 1), "invalid_message"},
		{"array", `[]`, "invalid_message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := acp.DecodePermission([]byte(tc.message), "generation-a", "session-a")
			wantCode(t, err, tc.code)
		})
	}
	for _, owner := range [][2]string{{"", "session-a"}, {"generation-a", ""}} {
		_, err := acp.DecodePermission([]byte(permission), owner[0], owner[1])
		wantCode(t, err, "invalid_owner")
	}
}

func TestUnicodeIdentitiesRemainLossless(t *testing.T) {
	for _, id := range []string{`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`} {
		_, err := acp.DecodePermission([]byte(strings.Replace(permission, `9007199254740993`, id, 1)), "generation-a", "session-a")
		wantCode(t, err, "invalid_message")
	}
	for _, id := range []string{`"\ud83d\ude00"`, `"literal-�"`, `"escaped-\\ud800"`, `"<request>"`} {
		o, err := acp.DecodePermission([]byte(strings.Replace(permission, `9007199254740993`, id, 1)), "generation-a", "session-a")
		if err != nil {
			t.Fatal(err)
		}
		b, err := o.PrepareCancelled(o.Fence())
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID string `json:"id"`
		}
		var original string
		if json.Unmarshal(b, &response) != nil || json.Unmarshal([]byte(id), &original) != nil || response.ID != original {
			t.Fatalf("string identity changed: %s", b)
		}
	}
	broken := append([]byte(permission[:len(permission)-1]), 0xff, '}')
	_, err := acp.DecodePermission(broken, "generation-a", "session-a")
	wantCode(t, err, "invalid_message")
}
func TestPermissionAliasIsolationAndConcurrentReads(t *testing.T) {
	b := []byte(permission)
	o, err := acp.DecodePermission(b, "generation-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := o.PrepareSelected(o.Fence(), "native-yes-42")
	if err != nil {
		t.Fatal(err)
	}
	for n := range b {
		b[n] = 'x'
	}
	opts := o.Options()
	opts[0].OptionID = "injected"
	opts[0].Kind = "allow_always"
	id := o.RequestID()
	id[0] = '0'
	tool := o.ToolCall()
	tool[0] = 'x'
	changedFence := o.Fence()
	changedFence.Revision = "injected"
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 32; k++ {
				out, err := o.PrepareSelected(o.Fence(), "native-yes-42")
				if err != nil || !bytes.Equal(out, baseline) {
					t.Errorf("immutable reply changed: %s %v", out, err)
					return
				}
				out[0] = 'x'
				options := o.Options()
				options[0].OptionID = "mutated"
				if !json.Valid(o.ToolCall()) || o.Options()[0].OptionID != "native-yes-42" {
					t.Error("aliased data")
					return
				}
			}
		}()
	}
	wg.Wait()
}
