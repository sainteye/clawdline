package projectassistantstore

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Store neither opens a database nor installs schema implicitly. Share a Store
// per database so WithCurrent and all memory writers share the same Bot lock.
type Store struct {
	db    *sql.DB
	locks sync.Map
}

func New(db *sql.DB) *Store { return &Store{db: db} }

var _ app.Repository = (*Store)(nil)
var _ app.Memory = (*Store)(nil)
var _ app.EventFeed = (*Store)(nil)

type aggregate struct {
	Bot          pa.Bot
	Conversation pa.Conversation
	Turns        map[string]pa.Turn
	Bindings     map[string]pa.ExecutionBinding
	Messages     []pa.Message
	Permissions  map[string]pa.PermissionOffer
	WorkLinks    map[string]pa.WorkLink
	Memory       pa.MemoryRevision
	Entries      map[string]pa.MemoryEntry
	ReadCursors  map[string]pa.ReadCursor
}

func failure(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }
func (s *Store) lock(key pa.BotKey) func() {
	v, _ := s.locks.LoadOrStore(key, &sync.Mutex{})
	m := v.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

type reader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func load(ctx context.Context, r reader, key pa.BotKey) (aggregate, error) {
	var data []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_bots WHERE machine=? AND bot=?`, key.MachineID, key.BotID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return aggregate{}, failure(pa.NotFound, "bot")
	}
	if err != nil {
		return aggregate{}, err
	}
	var a aggregate
	err = json.Unmarshal(data, &a)
	return a, err
}
func (s *Store) Bot(ctx context.Context, key pa.BotKey) (pa.Bot, error) {
	a, e := load(ctx, s.db, key)
	return a.Bot, e
}
func (s *Store) Conversation(ctx context.Context, key pa.BotKey, id string) (pa.Conversation, error) {
	a, e := load(ctx, s.db, key)
	if e != nil {
		return pa.Conversation{}, e
	}
	if a.Conversation.ID != id {
		return pa.Conversation{}, failure(pa.NotFound, "conversation")
	}
	return a.Conversation, nil
}
func (s *Store) Turn(ctx context.Context, key pa.BotKey, id string) (pa.Turn, error) {
	a, e := load(ctx, s.db, key)
	if e != nil {
		return pa.Turn{}, e
	}
	x, ok := a.Turns[id]
	if !ok {
		return x, failure(pa.NotFound, "turn")
	}
	return x, nil
}
func (s *Store) Binding(ctx context.Context, key pa.BotKey, id string) (pa.ExecutionBinding, error) {
	a, e := load(ctx, s.db, key)
	if e != nil {
		return pa.ExecutionBinding{}, e
	}
	x, ok := a.Bindings[id]
	if !ok {
		return x, failure(pa.NotFound, "binding")
	}
	return x, nil
}
func (s *Store) Permission(ctx context.Context, key pa.BotKey, id string) (pa.PermissionOffer, error) {
	a, e := load(ctx, s.db, key)
	if e != nil {
		return pa.PermissionOffer{}, e
	}
	x, ok := a.Permissions[id]
	if !ok {
		return x, failure(pa.NotFound, "permission")
	}
	return x, nil
}
func (s *Store) PendingTurns(ctx context.Context, key pa.BotKey) ([]pa.Turn, error) {
	a, err := load(ctx, s.db, key)
	if err != nil {
		return nil, err
	}
	out := []pa.Turn{}
	for _, t := range a.Turns {
		switch t.State {
		case pa.TurnAccepted, pa.TurnQueued, pa.TurnRunning, pa.TurnWaitingPermission, pa.TurnUnknown:
			out = append(out, t)
		}
	}
	order := map[string]int64{}
	for _, message := range a.Messages {
		order[message.ID] = message.Seq
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := order[out[i].MessageID], order[out[j].MessageID]
		if left == right {
			return out[i].ID < out[j].ID
		}
		return left < right
	})
	return out, nil
}
func (s *Store) Messages(ctx context.Context, q app.HistoryQuery) (app.MessagePage, error) {
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return app.MessagePage{}, err
	}
	if q.BeforeSeq < 0 {
		return app.MessagePage{}, failure(pa.Invalid, "before_seq")
	}
	a, err := load(ctx, s.db, q.Key)
	if err != nil {
		return app.MessagePage{}, err
	}
	if a.Conversation.ID != q.ConversationID {
		return app.MessagePage{}, failure(pa.NotFound, "conversation")
	}
	out := app.MessagePage{Items: []pa.Message{}}
	for i := len(a.Messages) - 1; i >= 0; i-- {
		m := a.Messages[i]
		if q.BeforeSeq > 0 && m.Seq >= q.BeforeSeq {
			continue
		}
		if len(out.Items) == q.PageSize {
			out.HasMore = true
			break
		}
		out.Items = append(out.Items, m)
		out.NextBeforeSeq = m.Seq
	}
	return out, nil
}
func receipt(ctx context.Context, r reader, c pa.Command) (*pa.Receipt, error) {
	if _, err := pa.DecideReplay(c, nil); err != nil {
		return nil, err
	}
	var data []byte
	err := r.QueryRowContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=? AND operation=? AND command_key=?`, c.Key.MachineID, c.Key.BotID, c.Operation, c.IdempotencyKey).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var x pa.Receipt
	if err = json.Unmarshal(data, &x); err != nil {
		return nil, err
	}
	_, err = pa.DecideReplay(c, &x)
	return &x, err
}
func (s *Store) Receipt(ctx context.Context, c pa.Command) (*pa.Receipt, error) {
	return receipt(ctx, s.db, c)
}
func writeReceipt(ctx context.Context, tx *sql.Tx, r pa.Receipt) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_receipts(machine,bot,operation,command_key,receipt_id,data) VALUES(?,?,?,?,?,?)`, r.Command.Key.MachineID, r.Command.Key.BotID, r.Command.Operation, r.Command.IdempotencyKey, r.ID, data)
	return err
}
func persist(ctx context.Context, tx *sql.Tx, a aggregate, expected int64) error {
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO project_assistant_bots(machine,bot,version,data) VALUES(?,?,?,?)`, a.Bot.Key.MachineID, a.Bot.Key.BotID, a.Bot.Version, data)
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE project_assistant_bots SET version=?,data=? WHERE machine=? AND bot=? AND version=?`, a.Bot.Version, data, a.Bot.Key.MachineID, a.Bot.Key.BotID, expected)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return failure(pa.StaleVersion, "bot")
	}
	return err
}
func (s *Store) Commit(ctx context.Context, m app.Mutation) (pa.Receipt, error) {
	unlock := s.lock(m.Command.Key)
	defer unlock()
	return s.commit(ctx, m)
}
func (s *Store) commit(ctx context.Context, m app.Mutation) (pa.Receipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, m.Command)
	if err != nil {
		return pa.Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	a, err := load(ctx, tx, m.Command.Key)
	if m.ExpectedVersion == 0 {
		if err == nil {
			return pa.Receipt{}, failure(pa.StaleVersion, "bot")
		}
		if pa.Code(err) != pa.NotFound {
			return pa.Receipt{}, err
		}
		if m.Bot == nil || m.Conversation == nil || m.Command.Operation != pa.CreateOperation {
			return pa.Receipt{}, failure(pa.Invalid, "create")
		}
		a = aggregate{Turns: map[string]pa.Turn{}, Bindings: map[string]pa.ExecutionBinding{}, Permissions: map[string]pa.PermissionOffer{}, WorkLinks: map[string]pa.WorkLink{}, Entries: map[string]pa.MemoryEntry{}, ReadCursors: map[string]pa.ReadCursor{}}
	} else {
		if err != nil {
			return pa.Receipt{}, err
		}
		if a.Bot.Version != m.ExpectedVersion {
			return pa.Receipt{}, failure(pa.StaleVersion, "bot")
		}
	}
	if err = applyMutation(&a, m); err != nil {
		return pa.Receipt{}, err
	}
	if err = validateAggregate(a); err != nil {
		return pa.Receipt{}, err
	}
	result := m.Receipt
	if result.Command != (pa.Command{}) && result.Command != m.Command {
		return pa.Receipt{}, failure(pa.ScopeDenied, "receipt_command")
	}
	result.Command = m.Command
	if result.ID == "" {
		return pa.Receipt{}, failure(pa.Invalid, "receipt_id")
	}
	if !validEffect(result.State) {
		return pa.Receipt{}, failure(pa.Invalid, "receipt_state")
	}
	for _, event := range m.Events {
		seq, e := appendEvent(ctx, tx, a, event)
		if e != nil {
			return pa.Receipt{}, e
		}
		result.EventSeq = seq
	}
	if err = persist(ctx, tx, a, m.ExpectedVersion); err != nil {
		return pa.Receipt{}, err
	}
	if err = writeReceipt(ctx, tx, result); err != nil {
		return pa.Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return pa.Receipt{}, err
	}
	return result, nil
}
func validEffect(s pa.EffectState) bool {
	switch s {
	case pa.EffectAccepted, pa.EffectExecuting, pa.EffectUnknown, pa.EffectDelivered, pa.EffectObserved, pa.EffectRead, pa.EffectFailed:
		return true
	}
	return false
}
func applyMutation(a *aggregate, m app.Mutation) error {
	if m.Bot != nil {
		b := *m.Bot
		if b.Key != m.Command.Key {
			return failure(pa.ScopeDenied, "bot")
		}
		if m.ExpectedVersion > 0 && (b.Project != a.Bot.Project || b.ConversationID != a.Bot.ConversationID || b.Agent != a.Bot.Agent || b.PermissionPolicy != a.Bot.PermissionPolicy) {
			return failure(pa.ScopeDenied, "immutable_bot")
		}
		if m.ExpectedVersion > 0 && (!b.CreatedAt.Equal(a.Bot.CreatedAt) || b.Instructions.Revision < a.Bot.Instructions.Revision || b.Instructions != a.Bot.Instructions && b.Instructions.Revision <= a.Bot.Instructions.Revision) {
			return failure(pa.StaleVersion, "bot_instructions")
		}
		a.Bot = b
	}
	a.Bot.Version = m.ExpectedVersion + 1
	if m.Conversation != nil {
		if m.ExpectedVersion > 0 && m.Conversation.ID != a.Conversation.ID {
			return failure(pa.ScopeDenied, "conversation")
		}
		if m.Conversation.Generation < a.Conversation.Generation || m.Conversation.LastMessageSeq < a.Conversation.LastMessageSeq {
			return failure(pa.StaleGeneration, "conversation")
		}
		if m.ExpectedVersion > 0 && m.Conversation.BindingID != a.Conversation.BindingID && m.Conversation.Generation <= a.Conversation.Generation {
			return failure(pa.StaleGeneration, "binding_generation")
		}
		a.Conversation = *m.Conversation
	}
	if a.Memory.Revision == 0 {
		a.Memory = pa.MemoryRevision{Key: a.Bot.Key, Project: a.Bot.Project, Revision: 1}
	}
	if m.Binding != nil {
		b := *m.Binding
		if old, ok := a.Bindings[b.Fence.BindingID]; ok && (old.Fence != b.Fence || old.Agent != b.Agent || old.Revoked && !b.Revoked) {
			return failure(pa.StaleGeneration, "binding")
		}
		a.Bindings[b.Fence.BindingID] = b
	}
	if m.Turn != nil {
		t := *m.Turn
		if old, ok := a.Turns[t.ID]; ok {
			capture := old.State == pa.TurnQueued && t.State == pa.TurnRunning
			if old.Fence.Key != t.Fence.Key || old.Fence.Project != t.Fence.Project || old.Fence.ConversationID != t.Fence.ConversationID || old.MessageID != t.MessageID || old.IdempotencyKey != t.IdempotencyKey || !capture && (old.Fence != t.Fence || old.Instructions != t.Instructions || old.MemoryRevision != t.MemoryRevision) {
				return failure(pa.ScopeDenied, "turn_snapshot")
			}
			if capture && (t.Fence.BindingID != a.Conversation.BindingID || t.Fence.Generation != a.Conversation.Generation || t.Instructions != a.Bot.Instructions || t.MemoryRevision != a.Memory.Revision) {
				return failure(pa.ContextStale, "turn_capture")
			}
			if old.State != t.State && !pa.CanTransition(old.State, t.State) {
				return failure(pa.StaleVersion, "turn_state")
			}
			// The running turn may end while the agent answers a steer, so a
			// joined turn names any turn of this conversation that started.
			joined := old.State == pa.TurnQueued && t.State == pa.TurnSucceeded
			target, found := a.Turns[t.JoinedTurnID]
			if joined && (t.JoinedTurnID == t.ID || !found || target.StartedAt == nil || target.Fence.ConversationID != t.Fence.ConversationID) || !joined && t.JoinedTurnID != old.JoinedTurnID {
				return failure(pa.ScopeDenied, "turn_joined")
			}
		}
		a.Turns[t.ID] = t
	}
	for _, msg := range m.Messages {
		for _, old := range a.Messages {
			if old.ID == msg.ID {
				return failure(pa.IdempotencyConflict, "message_id")
			}
		}
		next := int64(len(a.Messages) + 1)
		if msg.Seq == 0 {
			msg.Seq = next
		}
		if msg.Seq != next {
			return failure(pa.StaleVersion, "message_seq")
		}
		a.Messages = append(a.Messages, msg)
	}
	a.Conversation.LastMessageSeq = int64(len(a.Messages))
	for _, p := range m.Permissions {
		// Only a pending offer changes state, except that an answer recorded as
		// unknown before its native write becomes resolved once delivered.
		delivered := func(old pa.PermissionOffer) bool {
			return old.State == pa.PermissionUnknown && p.State == pa.PermissionResolved && old.SelectedOptionID == p.SelectedOptionID
		}
		if old, ok := a.Permissions[p.ID]; ok && (old.Fence != p.Fence || old.TurnID != p.TurnID || old.NativeRequestID != p.NativeRequestID || !reflect.DeepEqual(old.Options, p.Options) || old.State != pa.PermissionPending && old.State != p.State && !delivered(old)) {
			return failure(pa.StaleVersion, "permission")
		}
		a.Permissions[p.ID] = p
	}
	for _, w := range m.WorkLinks {
		if old, ok := a.WorkLinks[w.ID]; ok && (old.Fence != w.Fence || old.TurnID != w.TurnID || old.TaskID != w.TaskID) {
			return failure(pa.ScopeDenied, "work")
		}
		// WorkLink is a projection of trusted application/broker facts, not
		// an authorization to replay the dispatch. Later evidence may resolve
		// an unknown result or attach a Board link. The original command's
		// unknown receipt remains immutable and is still checked before this
		// mutation; scope, task ownership and aggregate CAS remain enforced.
		a.WorkLinks[w.ID] = w
	}
	// Memory changes require the provenance-aware Save/Forget entry points.
	if m.Memory != nil && !reflect.DeepEqual(*m.Memory, a.Memory) {
		return failure(pa.Invalid, "use_memory_port")
	}
	return nil
}
func validateAggregate(a aggregate) error {
	b := a.Bot
	if b.Key.MachineID == "" || b.Key.BotID == "" || b.Project.MachineID != b.Key.MachineID || b.ConversationID == "" || b.Version < 1 {
		return failure(pa.Invalid, "bot")
	}
	if err := pa.ValidateCreate(pa.CreateBot{Target: pa.ProjectSelection{MachineID: b.Key.MachineID, ProjectID: b.Project.ProjectID}, Name: b.Name, Appearance: b.Appearance, Agent: b.Agent, PermissionPolicy: b.PermissionPolicy, Instructions: b.Instructions.Text}, pa.ProjectResolution{Scope: b.Project, Directory: "host-resolved", Current: true, Available: true}); err != nil {
		return err
	}
	if b.Instructions.Revision < 1 {
		return failure(pa.Invalid, "instructions_revision")
	}
	c := a.Conversation
	if c.Key != b.Key || c.ID != b.ConversationID || c.Generation < 0 || c.LastMessageSeq != int64(len(a.Messages)) {
		return failure(pa.ScopeDenied, "conversation")
	}
	if (c.Generation == 0) != (c.BindingID == "") {
		return failure(pa.Invalid, "generation")
	}
	check := func(f pa.Fence) error {
		if f.Key != b.Key || f.Project != b.Project || f.ConversationID != c.ID {
			return failure(pa.ScopeDenied, "fact")
		}
		if f.Generation < 0 || f.Generation > c.Generation {
			return failure(pa.StaleGeneration, "fact")
		}
		if f.BindingID == "" {
			if f.Generation != 0 {
				return failure(pa.Invalid, "binding")
			}
			return nil
		}
		binding, ok := a.Bindings[f.BindingID]
		if !ok || binding.Fence != f {
			return failure(pa.ScopeDenied, "binding")
		}
		return nil
	}
	if c.BindingID != "" {
		binding, ok := a.Bindings[c.BindingID]
		if !ok || binding.Fence.Generation != c.Generation {
			return failure(pa.StaleGeneration, "current_binding")
		}
	}
	for id, binding := range a.Bindings {
		f := binding.Fence
		if id == "" || id != f.BindingID || binding.Agent != b.Agent || f.Generation < 1 || binding.MemoryRevision < 1 || binding.MemoryRevision > a.Memory.Revision || binding.InstructionRevision < 1 || binding.InstructionRevision > b.Instructions.Revision || binding.ForgottenThrough < 0 || binding.ForgottenThrough > binding.MemoryRevision {
			return failure(pa.Invalid, "binding")
		}
		if err := check(f); err != nil {
			return err
		}
	}
	pending := 0
	for id, t := range a.Turns {
		// A turn joined by Send now never ran on its own, so it has no captured snapshot.
		queued := t.State == pa.TurnQueued || t.State == pa.TurnAccepted || t.State == pa.TurnCancelled || t.State == pa.TurnFailed || t.JoinedTurnID != ""
		if id == "" || id != t.ID || t.MessageID == "" || t.IdempotencyKey == "" || t.MemoryRevision < 0 || t.MemoryRevision > a.Memory.Revision || !queued && (t.MemoryRevision < 1 || t.Instructions.Revision < 1) {
			return failure(pa.Invalid, "turn")
		}
		if err := check(t.Fence); err != nil {
			return err
		}
		switch t.State {
		case pa.TurnAccepted, pa.TurnQueued, pa.TurnRunning, pa.TurnWaitingPermission:
			pending++
		case pa.TurnSucceeded, pa.TurnFailed, pa.TurnCancelled, pa.TurnUnknown:
		default:
			return failure(pa.Invalid, "turn_state")
		}
	}
	if pending > pa.MaxPendingTurns {
		return failure(pa.CapacityFull, "pending_turns")
	}
	if c.ActiveTurnID != "" {
		t, ok := a.Turns[c.ActiveTurnID]
		if !ok || t.Fence.BindingID != c.BindingID || t.Fence.Generation != c.Generation {
			return failure(pa.ScopeDenied, "active_turn")
		}
	}
	ids := map[string]bool{}
	messageFacts := map[string]pa.Message{}
	for i, msg := range a.Messages {
		if msg.ID == "" || ids[msg.ID] || msg.Seq != int64(i+1) {
			return failure(pa.Invalid, "message")
		}
		if msg.QuoteMessageID != "" && !ids[msg.QuoteMessageID] {
			return failure(pa.ScopeDenied, "quote")
		}
		ids[msg.ID] = true
		messageFacts[msg.ID] = msg
		if err := check(msg.Fence); err != nil {
			return err
		}
		if msg.TurnID != "" {
			t, ok := a.Turns[msg.TurnID]
			if !ok || t.Fence.Key != msg.Fence.Key || t.Fence.Project != msg.Fence.Project || t.Fence.ConversationID != msg.Fence.ConversationID || (msg.Kind != pa.UserMessage && t.MessageID != msg.ID && t.Fence != msg.Fence) || msg.Fence.Generation > t.Fence.Generation {
				return failure(pa.ScopeDenied, "message_turn")
			}
		}
		switch msg.Kind {
		case pa.UserMessage, pa.AssistantMessage, pa.WorkMessage, pa.NoticeMessage:
		default:
			return failure(pa.Invalid, "message_kind")
		}
		if err := pa.ValidateMessage(msg.Text); err != nil {
			return err
		}
	}
	for _, t := range a.Turns {
		message, ok := messageFacts[t.MessageID]
		// A turn runs a person's message, or Clawdline's own resume notice
		// after a restart cut the previous turn off.
		if !ok || message.Kind != pa.UserMessage && message.Kind != pa.NoticeMessage || message.TurnID != t.ID {
			return failure(pa.ScopeDenied, "turn_message")
		}
	}
	for id, p := range a.Permissions {
		if id == "" || id != p.ID || p.NativeRequestID == "" {
			return failure(pa.Invalid, "permission")
		}
		switch p.State {
		case pa.PermissionPending, pa.PermissionResolved, pa.PermissionExpired, pa.PermissionCancelled, pa.PermissionUnknown:
		default:
			return failure(pa.Invalid, "permission_state")
		}
		options := map[string]bool{}
		for _, option := range p.Options {
			if option.ID == "" || options[option.ID] {
				return failure(pa.Invalid, "permission_option")
			}
			options[option.ID] = true
		}
		if p.SelectedOptionID != "" && !options[p.SelectedOptionID] {
			return failure(pa.Invalid, "permission_selection")
		}
		if err := check(p.Fence); err != nil {
			return err
		}
		t, ok := a.Turns[p.TurnID]
		if !ok || t.Fence != p.Fence {
			return failure(pa.ScopeDenied, "permission_turn")
		}
	}
	for id, w := range a.WorkLinks {
		if id == "" || id != w.ID || w.TaskID == "" {
			return failure(pa.Invalid, "work")
		}
		switch w.State {
		case pa.WorkAccepted, pa.WorkWaitingForPerson, pa.WorkExecuting, pa.WorkFailed, pa.WorkCompleted, pa.WorkLanded, pa.WorkUnknown:
		default:
			return failure(pa.Invalid, "work_state")
		}
		if err := check(w.Fence); err != nil {
			return err
		}
		t, ok := a.Turns[w.TurnID]
		if !ok || t.Fence != w.Fence {
			return failure(pa.ScopeDenied, "work_turn")
		}
		if w.ResultMessageID != "" && !ids[w.ResultMessageID] {
			return failure(pa.ScopeDenied, "work_message")
		}
	}
	if a.Memory.Key != b.Key || a.Memory.Project != b.Project {
		return failure(pa.ScopeDenied, "memory")
	}
	forgotten := map[string]bool{}
	for _, id := range a.Memory.ForgottenSourceIDs {
		if strings.TrimSpace(id) == "" || forgotten[id] {
			return failure(pa.Invalid, "memory_tombstone")
		}
		forgotten[id] = true
	}
	if err := pa.CheckContext(pa.ContextSnapshot{Memory: a.Memory, ProvenanceComplete: true}, a.Memory); err != nil {
		return err
	}
	for id, entry := range a.Entries {
		if id != entry.ID || entry.Key != b.Key || entry.Project != b.Project || entry.Revision < 1 || entry.Revision > a.Memory.Revision {
			return failure(pa.ScopeDenied, "memory_entry")
		}
		for _, source := range entry.Sources {
			if source.ID == "" || source.Key != b.Key || source.Project != b.Project || source.Revision < 1 || source.Revision > entry.Revision {
				return failure(pa.ScopeDenied, "memory_source")
			}
		}
		if !entry.Forgotten && contains(a.Memory.ForgottenSourceIDs, id) {
			return failure(pa.ContextStale, "forgotten_entry")
		}
		if !entry.Forgotten {
			if err := pa.CheckContext(pa.ContextSnapshot{Memory: a.Memory, Text: entry.Text, Sources: entry.Sources, ProvenanceComplete: true}, a.Memory); err != nil {
				return err
			}
		}
	}
	for id, r := range a.ReadCursors {
		if id != r.ReaderID || r.Key != b.Key || r.ConversationID != c.ID || r.ReaderID == "" || r.MessageSeq < 0 || r.MessageSeq > c.LastMessageSeq {
			return failure(pa.ScopeDenied, "read_cursor")
		}
	}
	return nil
}

type listCursor struct{ Machine, Query, LastMachine, LastBot string }

func (s *Store) List(ctx context.Context, q pa.ListBots) (pa.BotPage, error) {
	return s.ListForReader(ctx, q, "")
}

// ListForReader selects host-authenticated reader state. An empty reader gives
// no unread projection and never consults another reader's cursor.
func (s *Store) ListForReader(ctx context.Context, q pa.ListBots, readerID string) (pa.BotPage, error) {
	out := pa.BotPage{Items: []pa.BotSummary{}, Sources: []pa.SourceStatus{}}
	if err := pa.CheckPage(q.PageSize, false); err != nil {
		return out, err
	}
	cursor := listCursor{Machine: q.MachineID, Query: q.Query}
	if q.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil {
			return out, failure(pa.CursorExpired, "list_cursor")
		}
		if e = json.Unmarshal(raw, &cursor); e != nil || cursor.Machine != q.MachineID || cursor.Query != q.Query {
			return out, failure(pa.CursorExpired, "list_cursor")
		}
		var n int
		if e = s.db.QueryRowContext(ctx, `SELECT count(*) FROM project_assistant_bots WHERE machine=? AND bot=?`, cursor.LastMachine, cursor.LastBot).Scan(&n); e != nil {
			return out, e
		}
		if n != 1 {
			return out, failure(pa.CursorExpired, "list_cursor")
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT machine,bot,data FROM project_assistant_bots WHERE (?='' OR machine=?) AND (machine>? OR (machine=? AND bot>?)) ORDER BY machine,bot`, q.MachineID, q.MachineID, cursor.LastMachine, cursor.LastMachine, cursor.LastBot)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var machine, id string
		var raw []byte
		if err = rows.Scan(&machine, &id, &raw); err != nil {
			return out, err
		}
		var a aggregate
		if err = json.Unmarshal(raw, &a); err != nil {
			return out, err
		}
		if !strings.Contains(strings.ToLower(a.Bot.Name), strings.ToLower(q.Query)) {
			continue
		}
		if len(out.Items) == q.PageSize {
			last := out.Items[len(out.Items)-1].Bot.Key
			cursor.LastMachine = last.MachineID
			cursor.LastBot = last.BotID
			raw, _ = json.Marshal(cursor)
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		out.Items = append(out.Items, summary(a, readerID))
	}
	return out, rows.Err()
}
func summary(a aggregate, reader string) pa.BotSummary {
	r := pa.ReadCursor{Key: a.Bot.Key, ConversationID: a.Conversation.ID, ReaderID: reader}
	if reader != "" {
		if x, ok := a.ReadCursors[reader]; ok {
			r = x
		}
	}
	out := pa.BotSummary{Bot: a.Bot, Version: a.Bot.Version, ActivityAt: a.Bot.UpdatedAt, ReadCursor: r}
	for _, msg := range a.Messages {
		out.LatestMessage = msg.Text
		out.ActivityAt = msg.At
		if reader != "" && msg.Seq > r.MessageSeq {
			out.UnreadCount++
		}
	}
	for _, p := range a.Permissions {
		if p.State == pa.PermissionPending {
			out.PendingPermissions++
		}
	}
	if a.Conversation.ActiveTurnID != "" {
		out.Activity = string(a.Turns[a.Conversation.ActiveTurnID].State)
	}
	return out
}

func (s *Store) MarkRead(ctx context.Context, cmd pa.Command, in pa.ReadCursor) (pa.ReadCursor, error) {
	unlock := s.lock(cmd.Key)
	defer unlock()
	if cmd.Operation != pa.ReadOperation || cmd.Key != in.Key {
		return pa.ReadCursor{}, failure(pa.ScopeDenied, "read_command")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.ReadCursor{}, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, cmd)
	if err != nil {
		return pa.ReadCursor{}, err
	}
	a, err := load(ctx, tx, cmd.Key)
	if err != nil {
		return pa.ReadCursor{}, err
	}
	if prior != nil {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT data FROM project_assistant_events WHERE seq=? AND machine=? AND bot=?`, prior.EventSeq, cmd.Key.MachineID, cmd.Key.BotID).Scan(&raw); err != nil {
			return pa.ReadCursor{}, err
		}
		var event pa.Event
		if err = json.Unmarshal(raw, &event); err != nil {
			return pa.ReadCursor{}, err
		}
		if event.Read == nil {
			return pa.ReadCursor{}, failure(pa.Invalid, "read_receipt")
		}
		return *event.Read, nil
	}
	old, ok := a.ReadCursors[in.ReaderID]
	if !ok {
		old = pa.ReadCursor{Key: in.Key, ConversationID: a.Conversation.ID, ReaderID: in.ReaderID}
	}
	next, err := pa.AdvanceRead(old, in, a.Conversation.LastMessageSeq)
	if err != nil {
		return old, err
	}
	a.ReadCursors[in.ReaderID] = next
	expected := a.Bot.Version
	a.Bot.Version++
	f := pa.Fence{Key: a.Bot.Key, Project: a.Bot.Project, ConversationID: a.Conversation.ID, BindingID: a.Conversation.BindingID, Generation: a.Conversation.Generation}
	seq, err := appendEvent(ctx, tx, a, pa.Event{Version: pa.ContractVersion, ID: "read/" + in.ReaderID + "/" + cmd.IdempotencyKey, Fence: f, Kind: pa.ReadAdvanced, Read: &next, At: time.Now().UTC()})
	if err != nil {
		return old, err
	}
	if err = persist(ctx, tx, a, expected); err != nil {
		return old, err
	}
	if err = writeReceipt(ctx, tx, pa.Receipt{Command: cmd, ID: "read/" + cmd.IdempotencyKey, State: pa.EffectRead, ResourceID: in.ReaderID, EventSeq: seq}); err != nil {
		return old, err
	}
	return next, tx.Commit()
}
func (s *Store) Observe(ctx context.Context, cmd pa.Command, receiptID string, seq int64) (pa.Receipt, error) {
	unlock := s.lock(cmd.Key)
	defer unlock()
	if cmd.Operation != pa.ObserveOperation {
		return pa.Receipt{}, failure(pa.Invalid, "observe_command")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pa.Receipt{}, err
	}
	defer tx.Rollback()
	prior, err := receipt(ctx, tx, cmd)
	if err != nil {
		return pa.Receipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	if seq < 0 {
		return pa.Receipt{}, failure(pa.Invalid, "observe_seq")
	}
	rows, err := tx.QueryContext(ctx, `SELECT data FROM project_assistant_receipts WHERE machine=? AND bot=?`, cmd.Key.MachineID, cmd.Key.BotID)
	if err != nil {
		return pa.Receipt{}, err
	}
	var target *pa.Receipt
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			break
		}
		var r pa.Receipt
		if err = json.Unmarshal(data, &r); err != nil {
			break
		}
		if r.ID == receiptID {
			target = &r
			break
		}
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return pa.Receipt{}, err
	}
	if rowErr != nil {
		return pa.Receipt{}, rowErr
	}
	if target == nil {
		return pa.Receipt{}, failure(pa.NotFound, "receipt")
	}
	if target.State == pa.EffectUnknown || target.State == pa.EffectExecuting {
		return pa.Receipt{}, failure(pa.OutcomeUnknown, "receipt")
	}
	if seq < target.EventSeq {
		return pa.Receipt{}, failure(pa.Invalid, "observe_seq")
	}
	var last int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM project_assistant_events WHERE machine=? AND bot=?`, cmd.Key.MachineID, cmd.Key.BotID).Scan(&last); err != nil {
		return pa.Receipt{}, err
	}
	if seq > last {
		return pa.Receipt{}, failure(pa.Invalid, "observe_seq")
	}
	r := pa.Receipt{Command: cmd, ID: fmt.Sprintf("observe/%s", cmd.IdempotencyKey), State: pa.EffectObserved, ResourceID: receiptID, EventSeq: seq}
	if err = writeReceipt(ctx, tx, r); err != nil {
		return r, err
	}
	return r, tx.Commit()
}
