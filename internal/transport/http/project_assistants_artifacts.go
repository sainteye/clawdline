package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type paEncodedOutput struct {
	body  []byte
	check func(context.Context) error
}

func (h *projectAssistantHTTP) readOutput(r *http.Request, route paHTTPRoute) (any, error) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	q, err := paHTTPQuery(r, "machine_id", "conversation_id", "message_id", "id", "artifact_id", "offset")
	if err != nil {
		return nil, err
	}
	key, err := h.queryKey(q, route)
	if err != nil {
		return nil, err
	}
	offset, err := paHTTPNumber(q, "offset")
	if err != nil {
		return nil, err
	}
	if (q.Get("artifact_id") == "") == (q.Get("id") == "") || q.Get("conversation_id") == "" || q.Get("artifact_id") == "" && q.Get("message_id") == "" {
		return nil, paHTTPFailure(pa.Invalid, "output_scope")
	}
	result, err := h.options.Service.Output(r.Context(), key, q.Get("conversation_id"), q.Get("message_id"), q.Get("id"), q.Get("artifact_id"))
	if err != nil {
		return nil, err
	}
	data, err := h.options.Outputs.Chunk(r.Context(), key, result.Output, offset)
	if err != nil {
		return nil, err
	}
	source, err := paWireSourceStatus(result.Source)
	if err != nil {
		return nil, err
	}
	response := contract.ProjectAssistantOutputResponse{ContractVersion: pa.ContractVersion, Key: paWireBotKey(key), ConversationID: q.Get("conversation_id"), MessageID: result.MessageID, Output: paWireOutput(result.Output), Offset: offset, Data: base64.StdEncoding.EncodeToString(data), Source: source}
	body, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	if size > cloudops.ReadPlaintextBytesLimit {
		return nil, paHTTPFailure(pa.CapacityFull, "output_reply_bytes")
	}
	return paEncodedOutput{body: body, check: result.Check}, nil
}
