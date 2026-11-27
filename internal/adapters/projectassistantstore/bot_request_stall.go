package projectassistantstore

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// BotRequestHold is one open card that is not waiting for the person, and
// the Bot on this machine holding it.
type BotRequestHold struct {
	CardID string
	Bot    string
}

// Why a held card is stalled (docs/bot-request-cards.md §9.1). A card is
// stalled only on evidence that its last attempt went wrong and that nothing
// is working on it now; a Bot that simply has not acted yet is not stalled.
const (
	// BotRequestStallWorkFailed: the card's most recent dispatched work
	// failed or ended unknown.
	BotRequestStallWorkFailed = "work_failed"
	// BotRequestStallTurnUnsettled: the turn the handoff notice woke failed,
	// was stopped or ended unknown, and dispatched no work for the card.
	BotRequestStallTurnUnsettled = "turn_unsettled"
	// BotRequestStallTurnAndWork: that turn ended unsettled and the work it
	// dispatched for the card failed too.
	BotRequestStallTurnAndWork = "turn_and_work"
)

type botRequestWork struct {
	state  pa.WorkState
	turn   string
	at     string
	ord    int64
	active bool
}

// BotRequestStalls answers, for each held card that is stalled, why. A card
// is not stalled while its holder has a turn in flight or any work dispatched
// for the card is still active.
func (s *Store) BotRequestStalls(ctx context.Context, machine string, holds []BotRequestHold) (map[string]string, error) {
	out := map[string]string{}
	if len(holds) == 0 {
		return out, nil
	}
	ids := make([]any, 0, len(holds)+1)
	ids = append(ids, machine)
	for _, h := range holds {
		ids = append(ids, h.CardID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(CAST(data AS TEXT),'$.bot_request_id'),coalesce(json_extract(CAST(data AS TEXT),'$.state'),''),
 coalesce(json_extract(CAST(data AS TEXT),'$.turn_id'),''),coalesce(nullif(json_extract(CAST(data AS TEXT),'$.finished_at'),''),json_extract(CAST(data AS TEXT),'$.started_at'),''),ord
 FROM project_assistant_bot_facts WHERE machine=? AND collection='`+factWorkLinks+`'
 AND json_extract(CAST(data AS TEXT),'$.bot_request_id') IN (?`+strings.Repeat(",?", len(holds)-1)+`)`, ids...)
	if err != nil {
		return nil, err
	}
	latest := map[string]botRequestWork{}
	active := map[string]bool{}
	for rows.Next() {
		var card, state string
		var w botRequestWork
		if err = rows.Scan(&card, &state, &w.turn, &w.at, &w.ord); err != nil {
			rows.Close()
			return nil, err
		}
		w.state = pa.WorkState(state)
		if activeWorkLink(w.state) {
			active[card] = true
		}
		if prev, ok := latest[card]; !ok || w.at > prev.at || w.at == prev.at && w.ord > prev.ord {
			latest[card] = w
		}
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for _, h := range holds {
		if active[h.CardID] || h.Bot == "" {
			continue
		}
		busy, err := s.botTurnInFlight(ctx, machine, h.Bot)
		if err != nil {
			return nil, err
		}
		if busy {
			continue
		}
		noticeTurn, noticeState, err := s.botRequestNoticeTurn(ctx, machine, h)
		if err != nil {
			return nil, err
		}
		unsettled := noticeState == pa.TurnFailed || noticeState == pa.TurnCancelled || noticeState == pa.TurnUnknown
		work, dispatched := latest[h.CardID]
		switch {
		case dispatched && (work.state == pa.WorkFailed || work.state == pa.WorkUnknown):
			if unsettled && work.turn == noticeTurn {
				out[h.CardID] = BotRequestStallTurnAndWork
			} else {
				out[h.CardID] = BotRequestStallWorkFailed
			}
		case !dispatched && unsettled:
			out[h.CardID] = BotRequestStallTurnUnsettled
		}
	}
	return out, nil
}

// botTurnInFlight reports whether the Bot has a turn running, or one queued
// that will run on its own. A queue behind a turn a restart interrupted waits
// for the person's next message (app.RestartField), so it is not in flight.
func (s *Store) botTurnInFlight(ctx context.Context, machine, bot string) (bool, error) {
	var busy bool
	err := s.db.QueryRowContext(ctx, `WITH turns AS (SELECT json_extract(CAST(data AS TEXT),'$.state') state,
 coalesce(json_extract(CAST(data AS TEXT),'$.failure.field'),'') field FROM project_assistant_bot_facts WHERE machine=?1 AND bot=?2 AND collection='`+factTurns+`')
 SELECT coalesce((SELECT json_extract(CAST(data AS TEXT),'$.Conversation.active_turn_id') FROM project_assistant_bots WHERE machine=?1 AND bot=?2),'')<>''
 OR EXISTS(SELECT 1 FROM turns WHERE state IN ('running','waiting_permission'))
 OR EXISTS(SELECT 1 FROM turns WHERE state IN ('accepted','queued'))
  AND NOT EXISTS(SELECT 1 FROM turns WHERE state='unknown' AND field IN (?3,?4))`, machine, bot, app.RestartField, app.RestartConfirmedField).Scan(&busy)
	return busy, err
}

// botRequestNoticeTurn answers the turn the latest handoff notice for the
// card woke in its holder, and that turn's state; empty when none was woken.
func (s *Store) botRequestNoticeTurn(ctx context.Context, machine string, h BotRequestHold) (string, pa.TurnState, error) {
	prefix := pa.BotRequestHandoffPrefix + h.CardID + "-"
	rows, err := s.db.QueryContext(ctx, `SELECT command_key,coalesce(json_extract(CAST(data AS TEXT),'$.resource_id'),'') FROM project_assistant_receipts
 WHERE machine=? AND bot=? AND operation=? AND substr(command_key,1,?)=?`, machine, h.Bot, string(pa.SendOperation), len(prefix), prefix)
	if err != nil {
		return "", "", err
	}
	best, turn := int64(-1), ""
	for rows.Next() {
		var key, resource string
		if err = rows.Scan(&key, &resource); err != nil {
			rows.Close()
			return "", "", err
		}
		if v, e := strconv.ParseInt(strings.TrimPrefix(key, prefix), 10, 64); e == nil && v > best {
			best, turn = v, resource
		}
	}
	if err = rows.Close(); err != nil || turn == "" {
		return "", "", err
	}
	var state sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT json_extract(CAST(data AS TEXT),'$.state') FROM project_assistant_bot_facts WHERE machine=? AND bot=? AND collection='`+factTurns+`' AND member=?`,
		machine, h.Bot, turn).Scan(&state)
	if err == sql.ErrNoRows {
		return turn, "", nil
	}
	return turn, pa.TurnState(state.String), err
}
