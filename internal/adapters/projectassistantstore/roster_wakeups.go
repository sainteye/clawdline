package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"sync/atomic"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type rosterListener struct {
	machine string
	wake    chan struct{}
	lagged  atomic.Bool
}

type rosterBus struct {
	mu        sync.Mutex
	listeners map[*rosterListener]struct{}
}

func (s *Store) SubscribeRoster(machine string) (<-chan struct{}, func() bool, func(), error) {
	s.roster.mu.Lock()
	defer s.roster.mu.Unlock()
	if len(s.roster.listeners) >= pa.MaxRosterSubscribers {
		return nil, nil, nil, failure(pa.CapacityFull, "roster_subscribers")
	}
	if s.roster.listeners == nil {
		s.roster.listeners = make(map[*rosterListener]struct{})
	}
	l := &rosterListener{machine: machine, wake: make(chan struct{}, pa.MaxRosterWakeups)}
	s.roster.listeners[l] = struct{}{}
	return l.wake, l.lagged.Load, func() { s.roster.mu.Lock(); delete(s.roster.listeners, l); s.roster.mu.Unlock() }, nil
}

func (s *Store) rosterCommitted(machine string) {
	s.roster.mu.Lock()
	defer s.roster.mu.Unlock()
	for l := range s.roster.listeners {
		if l.machine != machine {
			continue
		}
		select {
		case l.wake <- struct{}{}:
		default:
			l.lagged.Store(true)
		}
	}
}

// This minimal journal survives aggregate/event deletion. It contains no
// conversation content, but remains private to authorized roster projections.
func appendRosterChange(ctx context.Context, tx *sql.Tx, a aggregate, removed bool) error {
	if a.BackgroundOwner != nil {
		return nil
	}
	project, err := json.Marshal(a.Bot.Project)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_roster_changes(machine,bot,project,conversation,generation,binding,removed) VALUES(?,?,?,?,?,?,?)`, a.Bot.Key.MachineID, a.Bot.Key.BotID, string(project), a.Bot.ConversationID, a.Conversation.Generation, a.Conversation.BindingID, removed)
	return err
}

func (s *Store) commitRoster(ctx context.Context, tx *sql.Tx, machine string) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	s.rosterCommitted(machine)
	return nil
}
