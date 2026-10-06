package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/productcopy"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func TestAssignmentErrorKeepsExistingFieldsAndProvenance(t *testing.T) {
	const detail = "This device may read, and not send."
	for _, tc := range []struct {
		name string
		err  error
		code string
		raw  bool
	}{
		{"fixed", &app.WorkError{Code: "forbidden", Message: detail}, "forbidden", false},
		{"typed raw collision", &app.WorkError{Code: "forbidden", Message: detail, RawMessage: true}, "forbidden", true},
		{"external collision", errors.New(detail), "assignment_failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := assignmentErrorWire(tc.err)
			if body["code"] != tc.code || body["message"] != detail {
				t.Fatalf("existing fields changed: %#v", body)
			}
			key, hasKey := body["detail_key"]
			if hasKey == tc.raw || hasKey && key != productcopy.HTTPRefusalKey(detail) {
				t.Fatalf("message origin lost: %#v", body)
			}
		})
	}
}

func TestRotationConfirmationKeepsExistingFieldsAndProvenance(t *testing.T) {
	const confirm = "post {\"confirm\": true} to rotate anyway"
	repair := map[string]string{"action": "review_devices"}
	for _, tc := range []struct {
		name string
		err  error
		raw  bool
	}{
		{"fixed sentinel", cloudtransport.ErrRotationUnconfirmed, false},
		{"wrapped external", fmt.Errorf("%w", cloudtransport.ErrRotationUnconfirmed), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := rotationConfirmationWire(tc.err, repair)
			refusal := body["error"].(map[string]any)
			if refusal["code"] != "confirm_required" || refusal["message"] != tc.err.Error() || body["confirm"] != confirm || !reflect.DeepEqual(body["repair"], repair) {
				t.Fatalf("existing fields changed: %#v", body)
			}
			key, hasKey := refusal["detail_key"]
			expected := productcopy.HTTPRefusalKey(tc.err.Error())
			if hasKey != (!tc.raw && expected != "") || hasKey && key != expected {
				t.Fatalf("message origin lost: %#v", body)
			}
		})
	}
}

func TestFlatRefusalKeepsWireFieldsAndAddsOnlyKnownDetailKey(t *testing.T) {
	const detail = "that is not a session action"
	w := httptest.NewRecorder()
	writeRefusal(w, http.StatusNotFound, "not_found", detail)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "not_found" || body["detail"] != detail || body["detail_key"] != productcopy.HTTPRefusalKey(detail) {
		t.Fatalf("flat refusal = %#v", body)
	}

	w = httptest.NewRecorder()
	writeRefusal(w, http.StatusBadGateway, "upstream_unavailable", "upstream task-xyz did not answer")
	body = nil
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, hasKey := body["detail_key"]; hasKey {
		t.Fatalf("dynamic refusal gained a key: %#v", body)
	}
}

func TestCloseRefusalKeepsReasonsAndAddsOnlyKnownDetailKey(t *testing.T) {
	const detail = "This Session still has unfinished work."
	reasons := []contract.CloseReason{{Code: "task_running"}}
	got := closeRefusalWire("close_blocked", detail, reasons)
	if got.Error != "close_blocked" || got.Detail != detail || got.DetailKey != productcopy.HTTPRefusalKey(detail) || len(got.Reasons) != 1 || got.Reasons[0].Code != "task_running" {
		t.Fatalf("close refusal = %#v", got)
	}
	unknown := closeRefusalWire("close_blocked", "An external process named abc is still running.", reasons)
	if unknown.DetailKey != "" {
		t.Fatalf("dynamic close refusal gained a key: %#v", unknown)
	}
}

func TestNestedRefusalsKeepRawMessageAndPutKeyInsideError(t *testing.T) {
	const detail = "This device may read, and not send."
	w := httptest.NewRecorder()
	writeAuthRefusal(w, http.StatusForbidden, "forbidden", detail)
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusForbidden || body.Error["code"] != "forbidden" || body.Error["message"] != detail || body.Error["detail_key"] != productcopy.HTTPRefusalKey(detail) {
		t.Fatalf("auth refusal = %d %#v", w.Code, body.Error)
	}

	w = httptest.NewRecorder()
	writeBrokerRefusal(w, orchestrator.Refusal{
		Status: http.StatusForbidden, Code: "forbidden", Message: detail,
		Extra: map[string]any{"detail_key": "http.untrusted", "blocking_task": "task-42"},
	})
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error["message"] != detail || body.Error["detail_key"] != productcopy.HTTPRefusalKey(detail) || body.Error["blocking_task"] != "task-42" {
		t.Fatalf("broker refusal = %#v", body.Error)
	}
}

func TestExternalDetailMatchingFixedCopyStillHasNoKey(t *testing.T) {
	const detail = "This device may read, and not send."
	if productcopy.HTTPRefusalKey(detail) == "" {
		t.Fatal("collision probe must use a catalogued sentence")
	}
	assertNoKey := func(name string, w *httptest.ResponseRecorder, nested bool) {
		t.Helper()
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		message := body
		if nested {
			message = body["error"].(map[string]any)
		}
		if _, ok := message["detail_key"]; ok {
			t.Fatalf("%s gave external copy a key: %#v", name, body)
		}
	}

	w := httptest.NewRecorder()
	writeRawRefusal(w, http.StatusForbidden, "forbidden", detail)
	assertNoKey("flat", w, false)
	w = httptest.NewRecorder()
	writeRawRefusalAbout(w, http.StatusBadGateway, "upstream_unreachable", detail, contract.Refusal{})
	assertNoKey("flat about", w, false)
	w = httptest.NewRecorder()
	writeRawAuthRefusal(w, http.StatusForbidden, "forbidden", detail)
	assertNoKey("auth", w, true)
	w = httptest.NewRecorder()
	writeBrokerRefusal(w, orchestrator.Refusal{Status: http.StatusForbidden, Code: "forbidden", Message: detail, RawMessage: true})
	assertNoKey("broker", w, true)
	if got := closeRefusalWireRaw("close_blocked", detail, nil); got.DetailKey != "" || got.Detail != detail {
		t.Fatalf("close refusal gave external copy a key: %#v", got)
	}
}

func TestWorkErrorPreservesFixedAndRuntimeMessageOrigins(t *testing.T) {
	const detail = "This device may read, and not send."
	if productcopy.HTTPRefusalKey(detail) == "" {
		t.Fatal("collision probe must use a catalogued sentence")
	}
	for _, tc := range []struct {
		name string
		raw  bool
		v1   bool
	}{
		{"v2 fixed", false, false},
		{"v2 raw collision", true, false},
		{"v1 current fixed", false, true},
		{"v1 current raw collision", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := &app.WorkError{Status: http.StatusForbidden, Code: "forbidden",
				Message: detail, RawMessage: tc.raw}
			w := httptest.NewRecorder()
			if tc.v1 {
				ref.Current = &app.WorkView{}
				writeWorkError(w, ref)
			} else {
				(&Server{}).writeWorkV2Error(w, ref)
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusForbidden || body["error"] != "forbidden" || body["detail"] != detail {
				t.Fatalf("wire refusal = %d %#v", w.Code, body)
			}
			key, hasKey := body["detail_key"]
			if hasKey == tc.raw || hasKey && key != productcopy.HTTPRefusalKey(detail) {
				t.Fatalf("message origin lost: %#v", body)
			}
		})
	}
}

func TestForwardedScheduleAndCoordinatorRefusalsKeepMessageOrigins(t *testing.T) {
	const detail = "This device may read, and not send."
	for _, tc := range []struct {
		name string
		raw  bool
		send func(*httptest.ResponseRecorder, bool)
	}{
		{"schedule fixed", false, func(w *httptest.ResponseRecorder, raw bool) {
			writeScheduleReply(w, app.ScheduleReply{Status: 403, Code: "forbidden", Message: detail, RawMessage: raw})
		}},
		{"schedule raw collision", true, func(w *httptest.ResponseRecorder, raw bool) {
			writeScheduleReply(w, app.ScheduleReply{Status: 403, Code: "forbidden", Message: detail, RawMessage: raw,
				Extra: map[string]any{"retry_after": 1}})
		}},
		{"coordinator fixed", false, func(w *httptest.ResponseRecorder, raw bool) {
			writeCoordinatorError(w, app.RoleRefusal{Status: 403, Code: "forbidden", Message: detail, RawMessage: raw})
		}},
		{"coordinator raw collision", true, func(w *httptest.ResponseRecorder, raw bool) {
			writeCoordinatorError(w, app.RoleRefusal{Status: 403, Code: "forbidden", Message: detail, RawMessage: raw})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.send(w, tc.raw)
			var body struct {
				Error map[string]any `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != 403 || body.Error["code"] != "forbidden" || body.Error["message"] != detail {
				t.Fatalf("wire refusal = %d %#v", w.Code, body.Error)
			}
			key, hasKey := body.Error["detail_key"]
			if hasKey == tc.raw || hasKey && key != productcopy.HTTPRefusalKey(detail) {
				t.Fatalf("message origin lost: %#v", body.Error)
			}
		})
	}
}
