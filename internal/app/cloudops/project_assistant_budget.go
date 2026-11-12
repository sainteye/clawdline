package cloudops

import (
	"encoding/json"
	"errors"
)

// ProjectAssistantReadReplySize measures the complete plaintext read reply with
// the same serializer used by Answer. RawMessage is marshaled again there, so
// HTML escaping and the wrapper must be included before a local page is sent.
// This pure measurement neither authorizes nor publishes a Cloud operation.
func ProjectAssistantReadReplySize(replyName string, status int, body json.RawMessage) (int, error) {
	if replyName == "" || status < 100 || status > 599 || !json.Valid(body) {
		return 0, errors.New("invalid project assistant read reply")
	}
	encoded := payload(replyName, status, body, nil)
	if len(encoded) == 0 {
		return 0, errors.New("could not serialize project assistant read reply")
	}
	return len(encoded), nil
}
