package projectassistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// MemoryReader projects only memory, without loading chat, receipts or exports.
type MemoryReader interface {
	ReadMemory(context.Context, pa.BotKey, string) (pa.MemoryViewState, pa.MemoryLayer, string, error)
}

// ProjectMemoryPage zero values retain the original unpaged read.
type ProjectMemoryPage struct {
	Number int
	Size   int
}

type MemoryReadRequest struct {
	Key                                                   pa.BotKey
	ConversationID, SourceEpoch, Layer, EntryID, Revision string
	Page                                                  ProjectMemoryPage
}
type MemoryReadResult struct {
	View        pa.MemoryView    `json:"view"`
	Detail      *pa.MemoryDetail `json:"detail,omitempty"`
	SourceEpoch string           `json:"source_epoch"`
	check       func(context.Context) error
}

func (r MemoryReadResult) Revalidate(ctx context.Context) error { return r.check(ctx) }

func (s *Service) memoryOwner(ctx context.Context, q MemoryReadRequest) (pa.Bot, Authority, pa.ProjectResolution, string, error) {
	a, err := s.readViewAuthority(ctx, q.Key)
	if err != nil {
		return pa.Bot{}, a, pa.ProjectResolution{}, "", err
	}
	var b pa.Bot
	if reader, ok := s.opts.Repository.(interface {
		MemoryBot(context.Context, pa.BotKey) (pa.Bot, error)
	}); ok {
		b, err = reader.MemoryBot(ctx, q.Key)
	} else {
		b, err = s.opts.Repository.Bot(ctx, q.Key)
	}
	if err != nil {
		return b, a, pa.ProjectResolution{}, "", err
	}
	if b.Key != q.Key || b.ConversationID != q.ConversationID {
		return b, a, pa.ProjectResolution{}, "", serviceFailure(pa.ScopeDenied, "memory_conversation")
	}
	resolved, err := s.opts.Projects.Resolve(ctx, pa.ProjectSelection{MachineID: q.Key.MachineID, ProjectID: b.Project.ProjectID})
	if err == nil {
		err = pa.ValidateTarget(pa.ProjectSelection{MachineID: q.Key.MachineID, ProjectID: b.Project.ProjectID}, resolved)
	}
	if err == nil && resolved.Scope != b.Project {
		err = serviceFailure(pa.ScopeDenied, "memory_project")
	}
	data, _ := json.Marshal(struct {
		Authority    string
		Project      pa.ProjectScope
		Conversation string
	}{readViewBinding(a), b.Project, b.ConversationID})
	h := sha256.Sum256(data)
	return b, a, resolved, hex.EncodeToString(h[:]), err
}

func (s *Service) ReadMemory(ctx context.Context, q MemoryReadRequest) (MemoryReadResult, error) {
	paged := q.Page != (ProjectMemoryPage{})
	projectPage := paged && q.Page.Number > 0
	if paged {
		if err := pa.CheckPage(q.Page.Size, false); err != nil {
			return MemoryReadResult{}, err
		}
		if q.Page.Number < 0 || projectPage && (q.Layer != "project" || q.SourceEpoch == "") ||
			!projectPage && q.Layer != "" && q.Layer != "bot" {
			return MemoryReadResult{}, serviceFailure(pa.Invalid, "project_memory_page")
		}
	}
	if q.EntryID == "" && (q.Revision != "" || !projectPage && (q.Layer != "" || q.SourceEpoch != "")) {
		return MemoryReadResult{}, serviceFailure(pa.Invalid, "memory_list")
	}
	b, _, resolved, epoch, err := s.memoryOwner(ctx, q)
	if err != nil {
		return MemoryReadResult{}, err
	}
	if q.SourceEpoch != "" && q.SourceEpoch != epoch {
		return MemoryReadResult{}, serviceFailure(pa.Revoked, "memory_source")
	}
	if q.EntryID != "" && (q.SourceEpoch == "" || q.Revision == "" || q.Layer != "bot" && q.Layer != "project") {
		return MemoryReadResult{}, serviceFailure(pa.Invalid, "memory_detail")
	}
	p, ok := s.opts.Repository.(MemoryReader)
	if !ok {
		return MemoryReadResult{}, serviceFailure(pa.Unsupported, "memory_view")
	}
	entry := ""
	if q.Layer == "bot" {
		entry = q.EntryID
	}
	m, local, text, err := p.ReadMemory(ctx, q.Key, entry)
	if err != nil {
		return MemoryReadResult{}, err
	}
	if m.Key != q.Key || m.Project != b.Project {
		return MemoryReadResult{}, serviceFailure(pa.ScopeDenied, "memory_identity")
	}
	shared := pa.MemoryLayer{Layer: "project", Entries: []pa.MemoryRow{}}
	sharedText := ""
	if s.opts.ProjectMemory != nil && (!paged || projectPage) {
		entry = ""
		if q.Layer == "project" {
			entry = q.EntryID
		}
		shared, sharedText, err = s.opts.ProjectMemory(ctx, resolved, entry, q.Page)
		if err != nil {
			return MemoryReadResult{}, err
		}
	}
	layers := []pa.MemoryLayer{shared, local}
	if paged {
		layers = []pa.MemoryLayer{local}
		if projectPage {
			layers = []pa.MemoryLayer{shared}
		}
	}
	out := MemoryReadResult{SourceEpoch: epoch, View: pa.MemoryView{Bot: b, ConversationID: b.ConversationID, Memory: m, Layers: layers}}
	if q.EntryID != "" {
		layer := local
		if q.Layer == "project" {
			layer = shared
			text = sharedText
		}
		if !layer.Available {
			return MemoryReadResult{}, serviceFailure(pa.Unsupported, "memory_layer")
		}
		if layer.Revision != q.Revision {
			return MemoryReadResult{}, serviceFailure(pa.ContextStale, "memory_revision")
		}
		out.Detail = &pa.MemoryDetail{Layer: q.Layer, ID: q.EntryID, Revision: layer.Revision, Text: text}
		if q.Layer == "bot" && len(layer.Entries) == 1 {
			row := layer.Entries[0]
			out.Detail.Kind, out.Detail.Title, out.Detail.Summary = row.Kind, row.Title, row.Text
		}
		out.View.Layers = []pa.MemoryLayer{} // detail does not repeat previews
	}
	out.check = func(ctx context.Context) error {
		current, _, _, e, err := s.memoryOwner(ctx, q)
		if err != nil {
			return err
		}
		if e != epoch || current.Project != b.Project {
			return serviceFailure(pa.Revoked, "memory_authority_changed")
		}
		_, currentLocal, _, err := p.ReadMemory(ctx, q.Key, "")
		if err != nil {
			return err
		}
		if currentLocal.Revision != local.Revision {
			return serviceFailure(pa.ContextStale, "memory_revision")
		}
		if s.opts.ProjectMemory != nil && (!paged || projectPage) {
			entry := ""
			if q.Layer == "project" {
				entry = q.EntryID
			}
			now, _, err := s.opts.ProjectMemory(ctx, resolved, entry, q.Page)
			if err != nil {
				return err
			}
			if now.Revision != shared.Revision {
				return serviceFailure(pa.ContextStale, "project_memory_revision")
			}
		}

		return ctx.Err()
	}
	return out, out.Revalidate(ctx)
}
