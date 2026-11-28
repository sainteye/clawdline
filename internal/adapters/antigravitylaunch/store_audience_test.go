package antigravitylaunch

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

func TestStorePreparePreservesChildAudienceThroughAdmit(t *testing.T) {
	f := setup(t)
	req := f.request
	req.Audience = projects.AudienceChild
	req.Language = "zh-Hant-TW"
	req.Memory = "Synthetic child memory"
	prepared, err := f.store.Prepare(context.Background(), req)
	if err != nil {
		t.Fatalf("Prepare child launch failed: %v", err)
	}
	if prepared.Request.Audience != projects.AudienceChild || prepared.Request.AntigravityContext == nil {
		t.Fatalf("prepared request lost child audience: %+v", prepared.Request)
	}
	if prepared.Request.AntigravityContext.Audience != projects.AudienceChild {
		t.Fatalf("prepared context lost child audience: %+v", prepared.Request.AntigravityContext)
	}
	if _, err := projects.Admit(prepared.Request); err != nil {
		t.Fatalf("projects.Admit rejected prepared child request: %v", err)
	}
	inst := prepared.Request.AntigravityContext.Instructions()
	if strings.Contains(inst, "clawdline note create") || strings.Contains(inst, "Clawdline Board item title") {
		t.Fatalf("child context leaked interactive rules: %s", inst)
	}
}
