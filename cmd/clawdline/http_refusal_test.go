package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/productcopy"
)

func TestCLIHTTPRefusalLocalizesFlatAndNestedHumanDetailOnly(t *testing.T) {
	const english = "that is not a session action"
	key := productcopy.HTTPRefusalKey(english)
	old := commandLanguage
	commandLanguage = "zh-Hant"
	t.Cleanup(func() { commandLanguage = old })

	flat, err := json.Marshal(map[string]any{"error": "bad_action", "detail": english, "detail_key": key})
	if err != nil {
		t.Fatal(err)
	}
	a := answer{Status: http.StatusBadRequest, Body: flat}
	code, shown := a.refusal()
	if code != "bad_action" || shown == english || !strings.Contains(shown, "Session") {
		t.Fatalf("flat refusal = %q %q", code, shown)
	}
	if !bytes.Equal(a.Body, flat) || bytes.Contains(a.Body, []byte(shown)) {
		t.Fatal("localized text changed the raw machine response")
	}
	if extras := a.refusalExtras(); len(extras) != 0 {
		t.Fatalf("detail_key leaked as a human extra: %v", extras)
	}

	nested, err := json.Marshal(map[string]any{"error": map[string]any{"code": "bad_action", "message": english, "detail_key": key}})
	if err != nil {
		t.Fatal(err)
	}
	a.Body = nested
	code, shown = a.refusal()
	if code != "bad_action" || shown == english || !strings.Contains(shown, "Session") {
		t.Fatalf("nested refusal = %q %q", code, shown)
	}
	if extras := a.refusalExtras(); len(extras) != 0 {
		t.Fatalf("nested detail_key leaked as a human extra: %v", extras)
	}
	res := &http.Response{Status: "400 Bad Request", Body: io.NopCloser(bytes.NewReader(nested))}
	if text := refusalText(res); !strings.Contains(text, shown) || !strings.Contains(text, "bad_action") {
		t.Fatalf("auth refusal text = %q", text)
	}
}

func TestCLIHTTPRefusalKeepsUntrustedOrDynamicDetailInEnglish(t *testing.T) {
	const english = "that is not a session action"
	key := productcopy.HTTPRefusalKey(english)
	for _, body := range []map[string]any{
		{"error": "bad_action", "detail": "dynamic instance 42"},
		{"error": "bad_action", "detail": "dynamic instance 42", "detail_key": key},
		{"error": "bad_action", "detail": english, "detail_key": "http.untrusted"},
	} {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		refusal, ok := parseCLIHTTPRefusal(data)
		if !ok || refusal.Code != "bad_action" || refusal.humanDetail("de") != body["detail"] {
			t.Fatalf("untrusted refusal %s -> %#v, %t", data, refusal, ok)
		}
	}
}

func TestItemAssignmentErrorUsesVerifiedKeyOnlyForHumanDetail(t *testing.T) {
	const english = "This device may read, and not send."
	key := productcopy.HTTPRefusalKey(english)
	translated := productcopy.HTTPRefusalText("zh-Hant", key, english)
	if key == "" || translated == english {
		t.Fatal("assignment probe needs a verified Traditional Chinese sentence")
	}
	old := commandLanguage
	commandLanguage = "zh-Hant"
	t.Cleanup(func() { commandLanguage = old })
	_, b := newStandIn(t, func(r *http.Request) (int, string) {
		if r.Method == http.MethodGet {
			return 200, epicItem
		}
		return 201, fmt.Sprintf(`{"ok":true,"assigned":false,
			"assignment_error":{"code":"forbidden","message":%q,"detail_key":%q},
			"item":{"id":"child-2","title":"Part","kind":"issue","phase":"created","version":2}}`, english, key)
	})
	var out, errs bytes.Buffer
	code := sessionItem(&out, &errs, b, "child", itemFlags{kind: "issue", title: "Part", description: "d",
		assign: assignFlags{open: true, assistant: "claude"}}, []string{"epic-1"}, thinConversation, "", envOf(nil))
	if code != 1 || !strings.Contains(errs.String(), "forbidden") || !strings.Contains(errs.String(), translated) ||
		strings.Contains(errs.String(), english) || !strings.Contains(out.String(), "child-2") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errs.String())
	}
}
