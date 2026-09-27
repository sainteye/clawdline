package http

import (
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
)

func TestProjectCatalogUsesTheCurrentBoardsProjectCounts(t *testing.T) {
	row := contract.CatalogProject{ID: "project-a", ItemCount: 503}
	got := summarizeProjectWork(row, "/project-a", map[string]store.WorkV2ProjectCount{
		"/project-a": {Items: 8, Open: 4},
	}, nil)
	if got.ItemCount != 8 || got.ActiveItemCount != 4 || got.SummaryCoverage != "complete" {
		t.Fatalf("current summary = %+v", got)
	}

	missing := summarizeProjectWork(row, "/project-a", nil, errors.New("store unavailable"))
	if missing.ItemCount != 503 || missing.ActiveItemCount != 0 || missing.SummaryCoverage != "unknown" {
		t.Fatalf("unavailable summary = %+v", missing)
	}
	unjoined := summarizeProjectWork(row, "", map[string]store.WorkV2ProjectCount{
		"/project-a": {Items: 8, Open: 4},
	}, nil)
	if unjoined.ItemCount != 503 || unjoined.SummaryCoverage != "unknown" {
		t.Fatalf("unjoined summary = %+v", unjoined)
	}
}
