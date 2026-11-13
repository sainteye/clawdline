package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// ReadViewResult carries facts and a publication fence. The caller must call
// Revalidate after serialization and immediately before publishing any bytes.
// Neither the epoch nor the SQL snapshot is proof of current human authority.
type ReadViewResult struct {
	View   ReadView
	Source pa.SourceStatus
	check  func(context.Context) error
}

func (r ReadViewResult) Revalidate(ctx context.Context) error {
	if r.check == nil {
		return serviceFailure(pa.ScopeDenied, "view_publication")
	}
	return r.check(ctx)
}

// ObservedAt is descriptive, not authority. Everything that determines the
// current source ceiling is bound, including write downgrades on a read page.
func readViewBinding(a Authority) string {
	b, _ := json.Marshal(struct {
		Principal, Reader, Revision, Machine string
		State                                pa.SourceState
		Access                               pa.Access
		Readable, Writable, Fresh            bool
		Feature                              pa.FeatureState
	}{a.PrincipalID, a.ReaderID, a.AuthorizationRevision, a.Source.MachineID,
		a.Source.State, a.Source.Access, a.Source.Readable, a.Source.Writable, a.Source.Fresh, a.Feature})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func (s *Service) readViewAuthority(ctx context.Context, key pa.BotKey) (Authority, error) {
	a, err := s.authorize(ctx, key, false)
	if err == nil && (a.Source.State != pa.SourceOnline || !a.Source.Fresh) {
		err = serviceFailure(pa.Offline, "view_source")
	}
	if err == nil && a.Source.Access != pa.Interactive && a.Source.Access != pa.HistoryOnly {
		err = serviceFailure(pa.Disabled, "view_source_access")
	}
	return a, err
}

// View is additive: missing composition refuses only this new projection.
// No Runtime, provider or aggregate Export is used for read initialization.
func (s *Service) View(ctx context.Context, q pa.ReadViewRequest) (ReadViewResult, error) {
	if err := pa.CheckReadViewRequest(q); err != nil {
		return ReadViewResult{}, err
	}
	a, err := s.readViewAuthority(ctx, q.Key)
	if err != nil {
		return ReadViewResult{}, err
	}
	if s.opts.ReadViews == nil || s.opts.ReadViewByteLimit <= 0 {
		return ReadViewResult{}, serviceFailure(pa.Unsupported, "read_views")
	}
	binding := readViewBinding(a)
	check := func(ctx context.Context) error {
		current, err := s.readViewAuthority(ctx, q.Key)
		if err != nil {
			return err
		}
		if readViewBinding(current) != binding {
			return serviceFailure(pa.Revoked, "view_authority_changed")
		}
		return ctx.Err()
	}
	view, err := s.opts.ReadViews.Read(ctx, ReadViewQuery{Request: q,
		AuthorizationBinding: binding, ByteLimit: s.opts.ReadViewByteLimit,
		CursorByteLimit: pa.MaxReadViewCursorBytes})
	if err != nil {
		return ReadViewResult{}, err
	}
	h := view.Head
	if h.Key != q.Key || h.Project.MachineID != q.Key.MachineID || pa.ValidateScope(h.Project) != nil ||
		h.Conversation.Key != q.Key || h.Conversation.ID != q.ConversationID ||
		h.CurrentFence.Key != q.Key || h.CurrentFence.Project != h.Project || h.CurrentFence.ConversationID != q.ConversationID ||
		h.CurrentFence.BindingID != h.Conversation.BindingID || h.CurrentFence.Generation != h.Conversation.Generation ||
		view.Mode != q.Mode || view.SourceEpoch == "" || (q.SourceEpoch != "" && q.SourceEpoch != view.SourceEpoch) {
		return ReadViewResult{}, serviceFailure(pa.ScopeDenied, "view_identity")
	}
	if view.Snapshot != nil && (view.Snapshot.Bot.Key != q.Key || view.Snapshot.Bot.Project != h.Project || view.Snapshot.Bot.ConversationID != q.ConversationID) {
		return ReadViewResult{}, serviceFailure(pa.ScopeDenied, "view_snapshot_identity")
	}
	if err = check(ctx); err != nil {
		return ReadViewResult{}, err
	}
	return ReadViewResult{View: view, Source: a.Source, check: check}, nil
}
