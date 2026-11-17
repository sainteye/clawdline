package projectassistant

import (
	"context"
	"strings"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Display metadata is projected onto copies; publication and event facts remain immutable.
func DisplayArtifactMessage(m pa.Message, titles map[string]pa.ArtifactDisplayTitle) pa.Message {
	if m.Output == nil || m.Output.ArtifactID == "" {
		return m
	}
	o := *m.Output
	o.TitleVersion = 1
	if title, ok := titles[o.ArtifactID]; ok {
		o.Name = title.Title
		o.TitleVersion = title.Version
	}
	m.Output = &o
	return m
}
func (x Export) DisplayMessages() []pa.Message {
	out := make([]pa.Message, len(x.Messages))
	for i, m := range x.Messages {
		out[i] = DisplayArtifactMessage(m, x.ArtifactTitles)
	}
	return out
}

type RenameArtifactRequest struct {
	Command                             pa.Command
	Fence                               pa.Fence
	ArtifactID, ExpectedOutputID, Title string
	ExpectedVersion                     int64
}

// RenameArtifact needs a human write grant and no native Agent invocation.
func (s *Service) RenameArtifact(ctx context.Context, q RenameArtifactRequest) (pa.Receipt, error) {
	a, err := s.authorize(ctx, q.Command.Key, true)
	if err != nil {
		return pa.Receipt{}, err
	}
	q.Title = strings.TrimSpace(q.Title)
	if !pa.ArtifactTitle(q.Title) || !pa.OutputName(q.ArtifactID) || q.ExpectedOutputID == "" || q.ExpectedVersion < 1 {
		return pa.Receipt{}, serviceFailure(pa.Invalid, "artifact_title")
	}
	if q.Fence.Key != q.Command.Key {
		return pa.Receipt{}, serviceFailure(pa.ScopeDenied, "artifact_fence")
	}
	c, err := canonicalCommand(q.Command, pa.RenameArtifactOperation, a.PrincipalID, struct {
		Fence                   pa.Fence
		Artifact, Output, Title string
		Version                 int64
	}{q.Fence, q.ArtifactID, q.ExpectedOutputID, q.Title, q.ExpectedVersion})
	if err != nil {
		return pa.Receipt{}, err
	}
	x, err := s.opts.Repository.Export(ctx, c.Key)
	if err != nil {
		return pa.Receipt{}, err
	}
	if err = checkHumanRoutineFence(q.Fence, currentFence(x.Bot, x.Conversation)); err != nil {
		return pa.Receipt{}, err
	}
	if err = s.routineProject(ctx, x.Bot); err != nil {
		return pa.Receipt{}, err
	}
	if prior, e := s.replay(ctx, c); e != nil {
		return pa.Receipt{}, e
	} else if prior != nil {
		if _, e = s.authorizeEffect(ctx, c.Key, true, a); e != nil {
			return pa.Receipt{}, e
		}
		return *prior, nil
	}
	var card *pa.Message
	for i := len(x.Messages) - 1; i >= 0; i-- {
		m := x.Messages[i]
		if m.Output != nil && m.Output.ArtifactID == q.ArtifactID {
			card = &m
			break
		}
	}
	if card == nil {
		return pa.Receipt{}, serviceFailure(pa.NotFound, "artifact")
	}
	effective := DisplayArtifactMessage(*card, x.ArtifactTitles)
	if effective.Output.ID != q.ExpectedOutputID || effective.Output.TitleVersion != q.ExpectedVersion {
		return pa.Receipt{}, serviceFailure(pa.StaleVersion, "artifact_title")
	}
	title := pa.ArtifactDisplayTitle{ArtifactID: q.ArtifactID, OutputID: q.ExpectedOutputID, Title: q.Title, Version: q.ExpectedVersion + 1}
	eventMessage := DisplayArtifactMessage(*card, map[string]pa.ArtifactDisplayTitle{q.ArtifactID: title})
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	if err = s.routineProject(ctx, x.Bot); err != nil {
		return pa.Receipt{}, err
	}
	if _, err = s.authorizeEffect(ctx, c.Key, true, a); err != nil {
		return pa.Receipt{}, err
	}
	return s.opts.Repository.Commit(ctx, Mutation{Command: c, ExpectedVersion: x.Bot.Version, ArtifactTitle: &title, Receipt: pa.Receipt{ID: s.opts.ID(), State: pa.EffectDelivered, ResourceID: q.ArtifactID}, Events: []pa.Event{{Version: pa.ContractVersion, ID: s.opts.ID(), Fence: card.Fence, Kind: pa.ArtifactRenamed, Message: &eventMessage, At: s.opts.Clock()}}})
}
