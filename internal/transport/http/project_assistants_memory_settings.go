package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
)

func (s *Server) projectAssistantProjectMemory(ctx context.Context, resolved pa.ProjectResolution, id string, page app.ProjectMemoryPage) (pa.MemoryLayer, string, error) {
	layer := pa.MemoryLayer{Layer: "project", Cap: memory.LiveEntryLimit, Available: true, Entries: []pa.MemoryRow{}}
	key, ok := memory.KeyFor(resolved.Directory)
	if !ok {
		return layer, "", paHTTPFailure(pa.ScopeDenied, "project_memory_key")
	}
	var v memory.BrowseView
	var total int
	var err error
	if page.Number > 0 {
		v, total, err = s.memoryStore().BrowsePage(key, id, page.Number, page.Size)
	} else {
		v, err = s.memoryStore().Browse(key, id)
		total = len(v.Entries)
	}
	if errors.Is(err, memory.ErrInvalid) && page.Number > 0 {
		return layer, "", paHTTPFailure(pa.Invalid, "project_memory_page")
	}
	if errors.Is(err, memory.ErrNotFound) {
		if page.Number > 0 {
			return layer, "", paHTTPFailure(pa.ContextStale, "project_memory_page")
		}
		return layer, "", paHTTPFailure(pa.NotFound, "memory_entry")
	}
	if err != nil {
		return layer, "", err
	}
	layer.Revision = v.Revision
	layer.Used = total
	layer.Truncated = layer.Used > memory.LiveEntryLimit
	for _, entry := range v.Entries {
		if len(layer.Entries) == memory.LiveEntryLimit {
			break
		}
		layer.Entries = append(layer.Entries, pa.MemoryRow{ID: entry.Entry.Name, Text: entry.Entry.Description, UpdatedAt: entry.UpdatedAt, Sources: []pa.MemorySource{}})
	}
	return layer, v.Text, ctx.Err()
}

type paEncodedMemorySettings struct {
	body  []byte
	check func(context.Context) error
}

func (h *projectAssistantHTTP) readMemorySettings(r *http.Request, route paHTTPRoute) (any, error) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	fields := []string{"machine_id", "conversation_id"}
	if route.action == "memory-view" {
		fields = append(fields, "source_epoch", "layer", "entry_id", "revision")
		if r.URL.Query().Has("page") || r.URL.Query().Has("page_size") {
			fields = append(fields, "page", "page_size")
		}
	} else {
		fields = append(fields, "agent", "model")
		if r.URL.Query().Has("login_profile") {
			fields = append(fields, "login_profile")
		}
	}
	q, err := paHTTPQuery(r, fields...)
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	request := app.MemoryReadRequest{Key: key, ConversationID: q.Get("conversation_id"), SourceEpoch: q.Get("source_epoch"), Layer: q.Get("layer"), EntryID: q.Get("entry_id"), Revision: q.Get("revision")}
	if q.Has("page") {
		page, pageErr := paHTTPNumber(q, "page")
		size, sizeErr := paHTTPNumber(q, "page_size")
		if pageErr != nil || sizeErr != nil || page > memory.LiveEntryLimit || size < 1 || size > pa.MaxPageRows {
			return nil, paHTTPFailure(pa.Invalid, "project_memory_page")
		}
		request.Page = app.ProjectMemoryPage{Number: int(page), Size: int(size)}
	}
	var value any
	var check func(context.Context) error
	if route.action == "memory-view" {
		v, e := h.options.Service.ReadMemory(r.Context(), request)
		if e != nil {
			return nil, e
		}
		value = struct {
			ContractVersion string `json:"contract_version"`
			app.MemoryReadResult
		}{pa.ContractVersion, v}
		check = v.Revalidate
	} else {
		profiles := []string{}
		if q.Has("login_profile") {
			profiles = append(profiles, q.Get("login_profile"))
		}
		v, c, e := h.options.Service.Configuration(r.Context(), request, pa.Agent(q.Get("agent")), q.Get("model"), profiles...)
		if e != nil {
			return nil, e
		}
		value = struct {
			ContractVersion string                  `json:"contract_version"`
			Catalog         pa.ConfigurationCatalog `json:"catalog"`
		}{pa.ContractVersion, v}
		check = c
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(bytes) > cloudops.ReadPlaintextBytesLimit {
		return nil, paHTTPFailure(pa.CapacityFull, "memory_settings_reply_bytes")
	}
	return paEncodedMemorySettings{bytes, check}, nil
}
