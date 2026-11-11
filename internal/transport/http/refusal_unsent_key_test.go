package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// On 2026-11-11 a Grok send of six large pictures answered 413
// pictures_too_large with no detail_key: the "nothing was typed" marker every
// refusal made before the first byte carries made its fixed sentence raw.
func TestARefusalBeforeTheFirstByteKeepsItsCatalogKey(t *testing.T) {
	const detail = "These pictures are too large to send together. Send fewer or smaller pictures in one message."
	if productcopy.HTTPRefusalKey(detail) == "" {
		t.Fatal("the fixed sentence is not in the refusal catalog")
	}
	for _, tc := range []struct {
		name  string
		cause error
		keyed bool
	}{
		{"nothing typed", terminal.Unsent{Why: detail}, true},
		{"runtime cause", errors.New("antigravity_process_frame_too_large"), false},
		{"unsent with other words", terminal.Unsent{Why: "the pane is gone"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeActionRefusal(rec, app.Refusal{Code: "pictures_too_large", Detail: detail, Cause: tc.cause})
			var body struct {
				Error, Detail string
				DetailKey     string `json:"detail_key"`
			}
			if rec.Code != http.StatusRequestEntityTooLarge || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error != "pictures_too_large" {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			if (body.DetailKey == productcopy.HTTPRefusalKey(detail)) != tc.keyed || !tc.keyed && body.DetailKey != "" {
				t.Fatalf("detail_key=%q, keyed want %v", body.DetailKey, tc.keyed)
			}
		})
	}
}
