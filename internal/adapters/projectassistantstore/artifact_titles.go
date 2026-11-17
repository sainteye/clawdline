package projectassistantstore

import (
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func applyArtifactTitle(a *aggregate, m app.Mutation) error {
	if m.ArtifactTitle == nil {
		return nil
	}
	t := *m.ArtifactTitle
	if m.Command.Operation != pa.RenameArtifactOperation || !pa.ArtifactTitle(t.Title) || !pa.OutputName(t.ArtifactID) || t.Version < 2 {
		return failure(pa.Invalid, "artifact_title")
	}
	var card *pa.Message
	for i := len(a.Messages) - 1; i >= 0; i-- {
		p := &a.Messages[i]
		if p.Output != nil && p.Output.ArtifactID == t.ArtifactID {
			card = p
			break
		}
	}
	if card == nil {
		return failure(pa.NotFound, "artifact")
	}
	effective := app.DisplayArtifactMessage(*card, a.ArtifactTitles)
	if card.Kind != pa.AssistantMessage || card.Fence.Key != m.Command.Key || card.Fence.Project != a.Bot.Project || card.Fence.ConversationID != a.Conversation.ID {
		return failure(pa.ScopeDenied, "artifact")
	}
	if effective.Output.ID != t.OutputID || effective.Output.TitleVersion+1 != t.Version {
		return failure(pa.StaleVersion, "artifact_title")
	}
	if a.ArtifactTitles == nil {
		a.ArtifactTitles = map[string]pa.ArtifactDisplayTitle{}
	}
	a.ArtifactTitles[t.ArtifactID] = t
	return nil
}
