package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/work"
)

func TestAPersonConvertsAPlanOverTheDedicatedRoute(t *testing.T) {
	s, _, seed := workV2AssignmentServer(t, session.StateIdle)
	plan, err := s.workV2().Create(context.Background(), app.NewWorkV2{
		ProjectID: "project-1", ProjectPath: seed.Item.ProjectPath, Kind: work.KindPlan,
		Title: "Future direction", Description: "Keep this out of current work until chosen.", Actor: "local",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/work/v2/items/" + plan.Item.ID + "/convert"
	body := fmt.Sprintf(`{"expected_version":%d,"kind":"feature"}`, plan.Item.Version)
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, path, body, "convert-plan"))
	if rec.Code != http.StatusOK {
		t.Fatalf("convert: %d %s", rec.Code, rec.Body)
	}
	var answer struct {
		Item workV2ItemWire `json:"item"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Item.Kind != "feature" || answer.Item.Area != "unassigned" || answer.Item.Title != plan.Item.Title {
		t.Fatalf("converted item = %+v", answer.Item)
	}

	replay := httptest.NewRecorder()
	s.workV2Route(replay, personWorkV2Request(http.MethodPost, path, body, "convert-plan"))
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotent-Replayed") != "true" ||
		replay.Body.String() != rec.Body.String() {
		t.Fatalf("replay: %d %s, headers=%v", replay.Code, replay.Body, replay.Header())
	}
}

func TestAPlanConversionNamesInvalidTargets(t *testing.T) {
	s, _, seed := workV2AssignmentServer(t, session.StateIdle)
	plan, err := s.workV2().Create(context.Background(), app.NewWorkV2{
		ProjectID: "project-1", ProjectPath: seed.Item.ProjectPath, Kind: work.KindPlan,
		Title: "Future direction", Description: "Keep this out of current work until chosen.", Actor: "local",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.workV2Route(rec, personWorkV2Request(http.MethodPost, "/v1/work/v2/items/"+plan.Item.ID+"/convert",
		fmt.Sprintf(`{"expected_version":%d,"kind":"refactor"}`, plan.Item.Version), "convert-plan-refactor"))
	if rec.Code != http.StatusConflict || codeOf(t, rec) != "conversion_kind_not_executable" {
		t.Fatalf("invalid target: %d %s", rec.Code, rec.Body)
	}
}
