package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type paEncodedReadView struct {
	body   []byte
	result app.ReadViewResult
}

func (h *projectAssistantHTTP) readView(r *http.Request, route paHTTPRoute) (any, error) {
	// GET has no request body, including a chunked body with unknown length.
	// Refuse framing before reading a stream that the caller may never finish.
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	if r.Body != nil {
		var b [1]byte
		n, err := r.Body.Read(b[:])
		if n != 0 || err != io.EOF {
			return nil, paHTTPFailure(pa.Invalid, "body")
		}
	}
	q, err := paHTTPQuery(r, "machine_id", "conversation_id", "mode", "source_epoch", "cursor", "after_seq", "page_size")
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	after, err := paHTTPNumber(q, "after_seq")
	if err != nil {
		return nil, err
	}
	size, err := paHTTPNumber(q, "page_size")
	if err != nil || size < 1 || size > pa.MaxEventPageRows {
		return nil, paHTTPFailure(pa.Invalid, "page_size")
	}
	request := pa.ReadViewRequest{Key: key, ConversationID: q.Get("conversation_id"), Mode: pa.ReadViewMode(q.Get("mode")),
		SourceEpoch: q.Get("source_epoch"), Cursor: q.Get("cursor"), AfterSeq: after, PageSize: int(size)}
	if err = pa.CheckReadViewRequest(request); err != nil {
		return nil, err
	}
	result, err := h.options.Service.View(r.Context(), request)
	if err != nil {
		return nil, err
	}
	head := &result.View.Head
	if h.options.NativeContext != nil && head.Conversation.ActiveTurnID != "" &&
		(request.Mode == pa.ReadViewSnapshot || request.Mode == pa.ReadViewEvents) {
		state, err := h.options.NativeContext(r.Context(), head.CurrentFence)
		if err == nil && state.UsageKnown && state.Fence == head.CurrentFence && state.TurnID == head.Conversation.ActiveTurnID {
			head.ContextUsage = &app.ReadContextUsage{Live: true, TurnID: state.TurnID, UsedTokens: state.UsedTokens, WindowTokens: state.WindowTokens}
		}
	}
	encoded, err := paPackReadView(request, result)
	if err != nil {
		return nil, err
	}
	return paEncodedReadView{body: encoded, result: result}, nil
}

func paReadViewEncode(header []byte, rows [][]byte) ([]byte, error) {
	if len(rows) == 0 {
		return append(header, '\n'), nil
	}
	const items = `"items":[]`
	start := bytes.Index(header, []byte(items))
	if start < 0 {
		return nil, paHTTPFailure(pa.Invalid, "view_items")
	}
	start += len(items) - 1
	size := len(header) + len(rows) // Commas and the final newline.
	for _, row := range rows {
		size += len(row)
	}
	encoded := make([]byte, 0, size)
	encoded = append(encoded, header[:start]...)
	for i, row := range rows {
		if i > 0 {
			encoded = append(encoded, ',')
		}
		encoded = append(encoded, row...)
	}
	encoded = append(encoded, header[start:]...)
	return append(encoded, '\n'), nil
}

func paPackReadView(q pa.ReadViewRequest, result app.ReadViewResult) ([]byte, error) {
	v := result.View
	source, err := paWireSourceStatus(result.Source)
	if err != nil {
		return nil, err
	}
	response := contract.ProjectAssistantReadViewResponse{ContractVersion: pa.ContractVersion,
		Mode: contract.ProjectAssistantReadViewMode(v.Mode), SourceEpoch: v.SourceEpoch,
		Head: paWireReadHead(v.Head), Source: source, Items: []contract.ProjectAssistantReadViewItem{}}
	if (v.Snapshot != nil) != (q.Mode == pa.ReadViewSnapshot) {
		return nil, paHTTPFailure(pa.Invalid, "view_snapshot")
	}
	if v.Snapshot != nil {
		response.Snapshot, err = paWireReadSnapshot(*v.Snapshot)
		if err != nil {
			return nil, err
		}
	}
	if q.Mode == pa.ReadViewEvents {
		response.NextSeq = q.AfterSeq
	}
	mandatory, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	name := "read:" + strings.Repeat("<", 128)
	cloudSize, err := cloudops.ProjectAssistantReadReplySize(name, http.StatusOK, mandatory)
	if err != nil {
		return nil, err
	}
	if len(mandatory)+1 > cloudops.ReadPlaintextBytesLimit || cloudSize > cloudops.ReadPlaintextBytesLimit {
		return nil, paHTTPFailure(pa.CapacityFull, "view_mandatory_bytes")
	}
	if len(v.Items) == 0 {
		if v.HasMore {
			return nil, paHTTPFailure(pa.Invalid, "view_empty_continuation")
		}
		return paReadViewEncode(mandatory, nil)
	}
	// The header and each item are compact, HTML-escaped json.Marshal output.
	// Cloud's RawMessage serializer leaves those bytes intact. The local final
	// newline is excluded from Cloud, while the reply envelope has a fixed cost.
	cloudOverhead := cloudSize - len(mandatory)
	var bestHeader []byte
	var bestCount int
	var failure error
	rows := make([][]byte, 0, len(v.Items))
	rowBytes := 0
	for i, item := range v.Items {
		// A bad row cannot be skipped. A preceding valid prefix still provides
		// its honest continuation and exposes this failure on the next read.
		if item.Failure != nil {
			if item.Failure.Code != pa.CapacityFull {
				return nil, item.Failure
			}
			failure = item.Failure
			break
		}
		wire, e := paWireReadItem(item)
		if e != nil {
			return nil, e
		}
		encodedItem, e := json.Marshal(wire)
		if e != nil {
			failure = e
			break
		}
		rowBytes += len(encodedItem)
		// Rows alone already exceed the complete reply ceiling. No shorter
		// later cursor or smaller envelope can rescue this or any later prefix.
		if rowBytes > cloudops.ReadPlaintextBytesLimit {
			failure = paHTTPFailure(pa.CapacityFull, "view_reply_bytes")
			break
		}
		rows = append(rows, encodedItem)
		response.HasMore = i+1 < len(v.Items) || v.HasMore
		response.NextCursor = ""
		if q.Mode == pa.ReadViewEvents {
			if item.Event == nil || item.Event.Seq <= response.NextSeq {
				return nil, paHTTPFailure(pa.Invalid, "view_event_seq")
			}
			response.NextSeq = item.Event.Seq
		} else if response.HasMore {
			// A later cursor can fit even when an earlier one could not. Keep
			// considering prefixes instead of rejecting all subsequent rows.
			if item.CursorFailure != nil {
				if item.CursorFailure.Code != pa.CapacityFull {
					return nil, item.CursorFailure
				}
				failure = item.CursorFailure
				continue
			}
			if item.AfterCursor == "" || len(item.AfterCursor) > pa.MaxReadViewCursorBytes {
				failure = paHTTPFailure(pa.CapacityFull, "view_cursor")
				continue
			}
			response.NextCursor = item.AfterCursor
		}
		header, e := json.Marshal(response)
		if e != nil {
			failure = e
			break
		}
		bodySize := len(header) + rowBytes + len(rows) - 1 // One comma between items.
		if bodySize+1 <= cloudops.ReadPlaintextBytesLimit && bodySize+cloudOverhead <= cloudops.ReadPlaintextBytesLimit {
			bestHeader, bestCount = header, len(rows)
		}
	}
	if bestHeader != nil {
		return paReadViewEncode(bestHeader, rows[:bestCount])
	}
	if failure != nil {
		return nil, failure
	}
	return nil, paHTTPFailure(pa.CapacityFull, "view_reply_bytes")
}
