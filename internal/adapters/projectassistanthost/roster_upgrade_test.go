package projectassistanthost

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestRosterDisabledExistingDataUpgradesBeforeHistoryRead(t *testing.T) {
	ctx := context.Background()
	f := newHostFixture(t)
	f.o.Reader = f.st.ProjectAssistantReader()
	f.o.ReadViewByteLimit = cloudops.ReadPlaintextBytesLimit
	f.start(t)
	f.enable(t)
	bot := f.create(t, "existing")
	if err := f.h.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// The previous feature schema had persisted data but no additive roster
	// journal. Leave it disabled; opening history must not enable execution.
	if _, err := f.o.Database.ExecContext(ctx, "DROP TABLE project_assistant_roster_changes"); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	s, err := f.h.Service()
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.SubscribeRoster(ctx, pa.RosterRequest{MachineID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	page, err := sub.Read(ctx, "")
	if err != nil {
		t.Fatal("disabled existing history cannot load the new roster", err)
	}
	if len(page.Page.Frame.Changes) != 1 || page.Page.Frame.Changes[0].Summary == nil || page.Page.Frame.Changes[0].Key != bot.Key {
		t.Fatal("existing Bot missing from upgraded roster", page.Page.Frame)
	}
	status, err := f.h.Status(ctx)
	if err != nil || status.Enabled || !status.HasData || f.factories.Load() != 0 {
		t.Fatal("history migration enabled execution or started a provider", status, err)
	}
}
