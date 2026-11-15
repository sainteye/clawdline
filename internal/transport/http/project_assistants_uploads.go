package http

import (
	"encoding/base64"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"net/http"
	"strings"
)

func paWireInput(f pa.InputFile) contract.ProjectAssistantInputFile {
	return contract.ProjectAssistantInputFile{ID: f.ID, Name: f.Name, Bytes: f.Bytes, Sha256: f.SHA256}
}

func (h *projectAssistantHTTP) uploadInput(w http.ResponseWriter, r *http.Request, route paHTTPRoute) (any, error) {
	// Base64 plus bounded metadata fits the existing one-MiB Cloud frame.
	q, err := paDecodeHTTPLimit[contract.ProjectAssistantUploadRequest](w, r, capacity.MaximumBotFrameBytes)
	if err != nil {
		return nil, err
	}
	command, err := h.command(r, route, q.Command, pa.UploadOperation)
	if err != nil {
		return nil, err
	}
	fence, err := paHTTPFence(command, q.Fence)
	if err != nil {
		return nil, err
	}
	if len(q.Data) > base64.StdEncoding.EncodedLen(pa.MaxInputChunkBytes) {
		return nil, paHTTPFailure(pa.CapacityFull, "upload_chunk")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(q.Data)
	if err != nil {
		return nil, paHTTPFailure(pa.Invalid, "upload_data")
	}
	file := pa.InputFile{ID: q.File.ID, Name: q.File.Name, Bytes: q.File.Bytes, SHA256: q.File.Sha256}
	lease, err := h.options.Service.InputUpload(r.Context(), fence, file.ID)
	if err != nil {
		return nil, err
	}
	offset, err := h.options.Inputs.Put(r.Context(), fence, file, q.Offset, data, q.Done, lease.Check, lease.Owner)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.ProjectAssistantUploadResponse{ContractVersion: pa.ContractVersion, Key: paWireBotKey(command.Key), File: paWireInput(file), Offset: offset, Done: q.Done})
	if err != nil {
		return nil, err
	}
	return paEncodedOutput{body: body, check: lease.Check}, nil
}

func (h *projectAssistantHTTP) readInput(r *http.Request, route paHTTPRoute) (any, error) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, paHTTPFailure(pa.Invalid, "body")
	}
	q, err := paHTTPQuery(r, "machine_id", "conversation_id", "message_id", "id", "offset")
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
	if !pa.ValidUploadID(q.Get("id")) || q.Get("message_id") == "" || q.Get("conversation_id") == "" {
		return nil, paHTTPFailure(pa.Invalid, "input_scope")
	}
	result, err := h.options.Service.Input(r.Context(), key, q.Get("conversation_id"), q.Get("message_id"), q.Get("id"))
	if err != nil {
		return nil, err
	}
	data, err := h.options.Inputs.Chunk(r.Context(), result.Fence, result.File, offset)
	if err != nil {
		return nil, err
	}
	source, err := paWireSourceStatus(result.Source)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(contract.ProjectAssistantInputResponse{ContractVersion: pa.ContractVersion, Key: paWireBotKey(key), ConversationID: q.Get("conversation_id"), MessageID: q.Get("message_id"), File: paWireInput(result.File), Offset: offset, Data: base64.StdEncoding.EncodeToString(data), Source: source})
	if err != nil {
		return nil, err
	}
	size, err := cloudops.ProjectAssistantReadReplySize("read:"+strings.Repeat("<", 128), http.StatusOK, body)
	if err != nil {
		return nil, err
	}
	if size > cloudops.ReadPlaintextBytesLimit {
		return nil, paHTTPFailure(pa.CapacityFull, "input_reply_bytes")
	}
	return paEncodedOutput{body: body, check: result.Check}, nil
}
