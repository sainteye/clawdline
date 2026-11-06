package agenthandoff

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// TaskBody is the encrypted instruction given to a target Agent. The target
// chooses its own Project directory from TaskStart.TargetProjectID.
type TaskBody struct {
	Assistant    string `json:"assistant"`
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
}

func ParseTaskBody(raw []byte) (TaskBody, error) {
	var body TaskBody
	if len(raw) == 0 || len(raw) > MaxBodyBytes {
		return body, InvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF ||
		!session.KnownAssistant(body.Assistant) ||
		strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Instructions) == "" ||
		strings.ContainsAny(body.Title, "\r\n\x00") {
		return TaskBody{}, InvalidRequest
	}
	if len(body.Title)+len(body.Instructions) > MaxBodyBytes {
		return TaskBody{}, InvalidRequest
	}
	return body, nil
}
