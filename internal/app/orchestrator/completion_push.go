package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/productcopy"
)

const (
	EffectSessionDeliveryPush = "session.delivery.push"
	EffectFanoutCompletedPush = "task.batch.completed.push"
)

type completionPush struct {
	Conversation string    `json:"conversation,omitempty"`
	After        time.Time `json:"after,omitempty"`
	Terminal     string    `json:"terminal"`
	Language     string    `json:"language"`
	Tag          string    `json:"tag"`
	Summary      string    `json:"summary,omitempty"`
	Count        int       `json:"count,omitempty"`
	Failed       int       `json:"failed,omitempty"`
	Titles       string    `json:"titles,omitempty"`
}

func (b *Broker) notificationLanguage() string {
	return productcopy.Resolve(b.ProductLanguage())
}

// settleDeliveryPush leaves an unattempted push pending for as long as the
// final answer takes. Time alone, missing sessions and retained idle rows are
// never evidence that the reporting turn finished. An unknown attempted push
// still follows ordinary recovery and is never sent again.
func (b *Broker) settleDeliveryPush(ctx context.Context, e store.Effect, rd reading) int {
	var p completionPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Terminal == "" || p.Tag == "" {
		return b.settleEffect(ctx, e, "") // The normal handler records malformed payloads.
	}
	skip := ""
	if p.Conversation == "" || p.After.IsZero() {
		// Pre-upgrade intents cannot prove which conversation owed the push.
		skip = "session_binding_missing"
	} else if s, ok := rd.session(p.Terminal); ok {
		if s.Observation.Freshness != session.FreshnessCurrent || !s.Observation.ObservedAt.After(p.After) {
			return 0
		}
		if s.ConversationID == "" {
			return 0
		}
		if s.ConversationID != p.Conversation {
			skip = "session_rebound"
		} else {
			if s.State != session.StateIdle || s.Menu != nil {
				return 0
			}
			switch s.Evidence {
			case session.EvidenceRegistry, session.EvidenceScreen, session.EvidenceStructured, session.EvidenceTranscript:
			default:
				return 0
			}
		}
	} else {
		if !rd.at.After(p.After) || !rd.sourceComplete(session.SourceForID(p.Terminal)) {
			return 0
		}
		skip = "session_gone"
	}
	if skip != "" {
		if !b.hold(e.ID) {
			return 0
		}
		defer b.release(e.ID)
		started, err := b.Store.StartEffect(ctx, e.ID)
		if err != nil {
			return 0
		}
		if err := b.finishEffect(ctx, started, effectResult{state: store.EffectDone, outcome: skip}); err != nil {
			log.Printf("orchestrator: delivery push %d could not be skipped: %v", e.ID, err)
			return 0
		}
		return 1
	}
	if _, err := b.runEffect(ctx, e.ID); err != nil {
		return 0
	}
	return 1
}

func runCompletionPush(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var p completionPush
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.Terminal == "" || p.Tag == "" {
		return effectResult{state: store.EffectFailed, outcome: "the effect carries no completion"}
	}
	key := "push_on_delivery"
	if e.Kind == EffectFanoutCompletedPush {
		key = "push_on_fanout"
	}
	if b.PushEnabled != nil && !b.PushEnabled(key) {
		return effectResult{state: store.EffectDone, outcome: "disabled_by_user"}
	}
	if b.Push == nil {
		return effectResult{state: store.EffectFailed, outcome: "this daemon cannot push"}
	}
	values := map[string]string{"count": strconv.Itoa(p.Count), "failed": strconv.Itoa(p.Failed)}
	title, body := productcopy.Format(p.Language, "delivery.title", nil), productcopy.Format(p.Language, "delivery.body", nil)
	if e.Kind == EffectFanoutCompletedPush {
		title, body = productcopy.Format(p.Language, "fanout.title", nil), productcopy.Format(p.Language, "fanout.body", values)
	}
	if b.PushEnabled != nil && b.PushEnabled("smart_notifications") {
		if e.Kind == EffectSessionDeliveryPush && p.Summary != "" {
			body = p.Summary
		} else if e.Kind == EffectFanoutCompletedPush && p.Titles != "" {
			body += " " + p.Titles
		}
	}
	sent, failed, err := b.Push(ctx, title, body, p.Terminal, p.Tag)
	result, _ := json.Marshal(map[string]any{"effect": e.ID, "terminal": p.Terminal, "sent": sent, "failed": failed})
	events := []store.Event{{Kind: strings.TrimSuffix(e.Kind, ".push") + ".pushed", Subject: e.Subject, Payload: result}}
	switch {
	case err != nil:
		return effectResult{state: store.EffectFailed, outcome: err.Error(), events: events}
	case sent == 0 && failed == 0:
		return effectResult{state: store.EffectDone, outcome: "not_subscribed", events: events}
	case sent == 0:
		return effectResult{state: store.EffectFailed, outcome: "no push service accepted it", events: events}
	default:
		return effectResult{state: store.EffectDone, outcome: "pushed", events: events}
	}
}

func (b *Broker) batchFinishedEffects(tx *store.Tx, finished Record) ([]store.Effect, error) {
	if finished.BatchID == "" || finished.RootTerminalID == "" {
		return nil, nil
	}
	rows, err := tx.BrokerBatchRows(finished.BatchID)
	if err != nil {
		return nil, err
	}
	count, failed := 0, 0
	var titles []string
	for _, row := range rows {
		r, err := Decode(row.Record)
		if err != nil {
			return nil, err
		}
		if r.BatchID != finished.BatchID || r.RootTerminalID != finished.RootTerminalID {
			continue
		}
		if r.ID == finished.ID {
			r = finished
		}
		if !r.State.Terminal() {
			return nil, nil
		}
		count++
		if r.State != StateSuccess {
			failed++
		}
		if len(titles) < 2 && r.Title != "" {
			titles = append(titles, r.Title)
		}
	}
	if count < 2 {
		return nil, nil
	}
	payload, _ := json.Marshal(completionPush{
		Terminal: finished.RootTerminalID, Language: b.notificationLanguage(),
		Tag: "task-batch-" + finished.BatchID, Count: count, Failed: failed,
		Titles: strings.Join(titles, ", "),
	})
	return []store.Effect{{Kind: EffectFanoutCompletedPush, Subject: finished.BatchID, Payload: payload}}, nil
}
