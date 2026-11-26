package projectassistanthost

import (
	"context"
	"log"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (h *Host) tickBackgroundTasks(ctx context.Context) {
	queues, err := h.store.BackgroundQueues(ctx, h.o.MachineID)
	if err != nil || len(queues) == 0 {
		return
	}
	runtime, err := h.runtime()
	if err != nil {
		return
	}
	for _, queue := range queues {
		if _, _, err = h.background(ctx, queue.Owner, true); err != nil {
			continue
		}
		for _, worker := range queue.Workers {
			h.recoverBackgroundWorker(ctx, runtime, worker)
		}
		for range pa.MaxBackgroundThreads {
			worker, err := h.store.StartBackgroundTask(ctx, queue.Owner, h.o.Clock())
			if err != nil {
				log.Printf("bots: background task start: %s", pa.Code(err))
				break
			}
			if worker.BotID == "" {
				break
			}
			h.recoverBackgroundWorker(ctx, runtime, worker)
		}
	}
}

func (h *Host) recoverBackgroundWorker(ctx context.Context, runtime *app.Engine, worker pa.BotKey) {
	if err := runtime.Recover(ctx, worker); err != nil {
		log.Printf("bots: background task recovery: %s", pa.Code(err))
	}
}

// expireWaitingDecisions completes peer exchanges whose question for the
// person expired unanswered, so the requesting Bot hears it and continues.
func (h *Host) expireWaitingDecisions(ctx context.Context, at time.Time) error {
	runtime, err := h.runtime()
	if err != nil {
		return err
	}
	if err = runtime.ExpireWaitingDecisions(ctx, h.o.MachineID, at); err != nil {
		log.Printf("bots: expire waiting questions: %s", pa.Code(err))
	}
	return err
}
