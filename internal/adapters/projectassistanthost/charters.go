package projectassistanthost

import (
	"context"
	"time"

	featurestore "github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// hostCharters delivers each Project's team charter from the Host's own
// store: every prompt reads the current version, and every sent prompt
// records which version it carried for the charter's readers.
type hostCharters struct {
	store *featurestore.Store
	clock func() time.Time
}

func (c hostCharters) CurrentCharter(ctx context.Context, project pa.ProjectScope) (int64, string, error) {
	v, err := c.store.Charter(ctx, project.MachineID, project.ProjectID)
	return v.Version, v.Text, err
}

func (c hostCharters) CharterDelivered(ctx context.Context, bot pa.Bot, turnID string, version int64) error {
	now := time.Now()
	if c.clock != nil {
		now = c.clock()
	}
	return c.store.RecordCharterRead(ctx, bot.Project.MachineID, bot.Project.ProjectID, bot.Key.BotID, version, turnID, now)
}
