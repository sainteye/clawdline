package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

// The assistants route is the check a root runs before choosing whom to
// dispatch to, and every answer it can give has to be usable: a row either
// carries windows, or says which kind of nothing it has. An `unknown` with no
// reason is the answer this route existed to stop giving.
//
// It reads this machine's own files, so the assertions are the ones that hold
// whatever those files say today.
func TestEveryAssistantRowIsUsable(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).brokerAssistants(rec, httptest.NewRequest(http.MethodGet, "/v1/orchestrator/assistants", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var list contract.AssistantList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Assistants) == 0 {
		t.Fatal("no assistants")
	}
	reasons := map[contract.AssistantUnknownReason]bool{
		"no_record": true, "no_reading": true, "unreadable": true, "too_old": true,
	}
	for _, row := range list.Assistants {
		if row.Detail == "" {
			t.Errorf("%s: no sentence to print", row.ID)
		}
		switch row.Availability {
		case contract.AssistantAvailabilityUnknown:
			if !reasons[row.UnknownReason] {
				t.Errorf("%s: unknown with reason %q", row.ID, row.UnknownReason)
			}
		default:
			if row.UnknownReason != "" {
				t.Errorf("%s: %s carries an unknown reason %q", row.ID, row.Availability, row.UnknownReason)
			}
			if len(row.Windows) == 0 {
				t.Errorf("%s: %s with no window", row.ID, row.Availability)
			}
		}
		// A number on screen comes with its age, and the age comes with the
		// line it is judged against.
		if row.ObservedAt == nil {
			if row.AgeSeconds != nil || row.FreshForSeconds != nil || row.Stale {
				t.Errorf("%s: nothing was read and it has an age: %+v", row.ID, row)
			}
			continue
		}
		if row.AgeSeconds == nil || *row.AgeSeconds < 0 {
			t.Errorf("%s: a reading with no age: %v", row.ID, row.AgeSeconds)
			continue
		}
		if row.FreshForSeconds == nil || *row.FreshForSeconds <= 0 {
			t.Errorf("%s: a reading with no line to age against: %v", row.ID, row.FreshForSeconds)
			continue
		}
		if row.Stale != (*row.AgeSeconds > *row.FreshForSeconds) {
			t.Errorf("%s: stale=%v at %ds against a %ds line",
				row.ID, row.Stale, *row.AgeSeconds, *row.FreshForSeconds)
		}
	}
	if list.At < time.Now().Add(-time.Minute).Unix() {
		t.Errorf("the answer is stamped %d", list.At)
	}
}

func TestAntigravityInstallationNeverClaimsAuthenticationOrInteractiveReadiness(t *testing.T) {
	for _, installed := range []bool{false, true} {
		for _, compatible := range []bool{false, true} {
			info := antigravity.Installation{InstalledKnown: true, Installed: installed, Compatible: compatible, AuthState: antigravity.AuthUnknown}
			if installed && compatible {
				info.Version = "1.4.5"
			}
			row := antigravityStartAssistant(info)
			if row.ID != "antigravity" || row.AuthState != contract.StartAuthStateUnknown || row.Availability != contract.StartAvailabilityUnknown {
				t.Fatalf("%+v", row)
			}
			if row.Installed == nil || *row.Installed != installed {
				t.Fatalf("installed lost: %+v", row)
			}
			if compatible && row.IntegrationState != contract.StartIntegrationStateDisabled {
				t.Fatalf("compatible CLI was reported missing: %+v", row)
			}
			if row.Capabilities == nil || *row.Capabilities != (contract.StartCapabilities{}) {
				t.Fatalf("invented capabilities: %+v", row)
			}
			raw, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["installed"] == nil || string(wire["installed"]) == "null" {
				t.Fatalf("false became unknown: %s", raw)
			}
		}
	}
}

func TestAntigravityFoundationRoutesNeverFallThroughToOtherProviders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	for _, operation := range []string{"start", "machine", "resume", "history"} {
		rec := httptest.NewRecorder()
		server := &Server{}
		switch operation {
		case "start":
			server.startPlace(rec, request, "place", "antigravity", "", "")
		case "machine":
			server.startPlace(rec, request, projects.MachinePlaceID, "antigravity", "", "")
		case "resume":
			server.resumePlace(rec, request, "place", "antigravity", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "")
		case "history":
			server.pastRoute(rec, request, "place", "antigravity")
		}
		var got contract.PlaceRefusal
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusConflict || got.Error.Code != "provider_integration_unavailable" || got.Error.DetailKey != "http.1db27562f4ac9522" {
			t.Fatalf("%s: %d %s", operation, rec.Code, rec.Body.String())
		}
	}
	if assistantNeedsSignIn(request.Context(), "antigravity") {
		t.Fatal("unknown authentication became signed out")
	}
}
