package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"sort"
	"strings"
)

// InputUpload captures current operate authority and the exact draft epoch.
// Every chunk and final acknowledgement rechecks it. A message's transaction
// is the binding receipt; no separate file mutation can lose an uncertain send.
type InputOwner struct {
	Lifecycle string
	Revision  int64
	Authority string
}
type InputUploadLease struct {
	Owner InputOwner
	Check func(context.Context) error
}

func (s *Service) InputUpload(ctx context.Context, fence pa.Fence, id string) (InputUploadLease, error) {
	if !pa.ValidUploadID(id) {
		return InputUploadLease{}, serviceFailure(pa.Invalid, "upload_id")
	}
	a, err := s.authorize(ctx, fence.Key, true)
	if err != nil {
		return InputUploadLease{}, err
	}
	initial, err := s.opts.Repository.Export(ctx, fence.Key)
	if err != nil {
		return InputUploadLease{}, err
	}
	owner := InputOwner{Revision: initial.Bot.Instructions.Revision, Authority: readViewBinding(a), Lifecycle: inputLifecycle(initial)}
	check := func(ctx context.Context) error {
		if _, err := s.authorizeEffect(ctx, fence.Key, true, a); err != nil {
			return err
		}
		x, err := s.opts.Repository.Export(ctx, fence.Key)
		if err != nil {
			return err
		}
		if x.Bot.Instructions.Revision != owner.Revision || inputLifecycle(x) != owner.Lifecycle {
			return serviceFailure(pa.StaleVersion, "upload_configuration")
		}
		if err = pa.CheckSendFence(fence, currentFence(x.Bot, x.Conversation)); err != nil {
			return err
		}
		for _, m := range x.Messages {
			for _, f := range m.Files {
				if f.ID == id {
					return serviceFailure(pa.IdempotencyConflict, "upload_bound")
				}
			}
		}
		return ctx.Err()
	}
	return InputUploadLease{Owner: owner, Check: check}, check(ctx)
}

type InputRead struct {
	File   pa.InputFile
	Fence  pa.Fence
	Source pa.SourceStatus
	Check  func(context.Context) error
}

func (s *Service) Input(ctx context.Context, key pa.BotKey, conversation, message, id string) (InputRead, error) {
	a, err := s.readViewAuthority(ctx, key)
	if err != nil {
		return InputRead{}, err
	}
	binding := readViewBinding(a)
	lookup := func(ctx context.Context) (pa.InputFile, pa.Fence, error) {
		x, e := s.opts.Repository.Export(ctx, key)
		if e != nil {
			return pa.InputFile{}, pa.Fence{}, e
		}
		if x.Bot.Key != key || x.Conversation.ID != conversation {
			return pa.InputFile{}, pa.Fence{}, serviceFailure(pa.ScopeDenied, "input_conversation")
		}
		for _, m := range x.Messages {
			if m.ID != message {
				continue
			}
			if m.Kind != pa.UserMessage || m.Fence.Project != x.Bot.Project || m.Fence.Key != key || m.Fence.ConversationID != conversation {
				break
			}
			for _, t := range x.Turns {
				if t.ID == m.TurnID && t.State == pa.TurnCancelled && t.Failure == nil {
					return pa.InputFile{}, pa.Fence{}, serviceFailure(pa.NotFound, "input_withdrawn")
				}
			}
			for _, f := range m.Files {
				if f.ID == id {
					return f, m.Fence, nil
				}
			}
		}
		return pa.InputFile{}, pa.Fence{}, serviceFailure(pa.NotFound, "input_message")
	}
	file, fence, err := lookup(ctx)
	if err != nil {
		return InputRead{}, err
	}
	check := func(ctx context.Context) error {
		current, e := s.readViewAuthority(ctx, key)
		if e != nil {
			return e
		}
		if readViewBinding(current) != binding {
			return serviceFailure(pa.Revoked, "input_authority_changed")
		}
		f, owner, e := lookup(ctx)
		if e != nil {
			return e
		}
		if f != file || owner != fence {
			return serviceFailure(pa.Revoked, "input_changed")
		}
		return ctx.Err()
	}
	return InputRead{File: file, Fence: fence, Source: a.Source, Check: check}, check(ctx)
}

// Stop intents are durable even when their acknowledgement was lost. Hash
// their identities so every pre-Stop staging lease remains invalid on restart.
func inputLifecycle(x Export) string {
	ids := []string{}
	for _, r := range x.Receipts {
		if r.Command.Operation == pa.StopOperation {
			ids = append(ids, r.ID)
		}
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return hex.EncodeToString(sum[:])
}

// A failed cleanup leaves the durable Stop receipt intact. Retrying that same
// command replays the cancellation and retries only this idempotent cleanup.
func (r *Engine) discardTakenBackInputs(ctx context.Context, q StopRequest, receipt pa.Receipt) error {
	remover, ok := r.o.Inputs.(InputRemover)
	if !ok || q.TurnID == "" || receipt.State != pa.EffectDelivered || receipt.ResourceID != q.TurnID {
		return nil
	}
	t, err := r.o.Repository.Turn(ctx, q.Command.Key, q.TurnID)
	if err != nil {
		return err
	}
	if t.State != pa.TurnCancelled || t.Failure != nil || t.StartedAt != nil || t.JoinedTurnID != "" {
		return nil
	}
	if t.Fence != q.Fence {
		return runtimeFailure(pa.ScopeDenied, "input_takeback_fence")
	}
	x, err := r.o.Repository.Export(ctx, q.Command.Key)
	if err != nil {
		return err
	}
	for _, m := range x.Messages {
		if m.ID != t.MessageID || m.TurnID != t.ID || m.Kind != pa.UserMessage {
			continue
		}
		if m.Fence != t.Fence {
			return runtimeFailure(pa.ScopeDenied, "input_takeback_message")
		}
		// Do not remove a malformed shared reference that another message may
		// already have handed to its native execution.
		for _, file := range m.Files {
			for _, other := range x.Messages {
				if other.ID == m.ID {
					continue
				}
				for _, shared := range other.Files {
					if shared.ID == file.ID {
						return runtimeFailure(pa.ScopeDenied, "input_takeback_shared")
					}
				}
			}
		}
		for _, file := range m.Files {
			if err = remover.Remove(ctx, m.Fence, file); err != nil {
				return err
			}
		}
		return nil
	}
	return runtimeFailure(pa.NotFound, "input_takeback_message")
}
