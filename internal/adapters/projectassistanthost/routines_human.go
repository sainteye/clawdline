package projectassistanthost

import (
	"context"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (f runtimeFacade) CheckHumanRoutine(ctx context.Context, bot pa.Bot, fence pa.Fence) error {
	r, err := f.h.runtime()
	if err != nil {
		return err
	}
	return r.CheckHumanRoutine(ctx, bot, fence)
}
