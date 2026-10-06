package main

import (
	"encoding/json"
	"strings"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/productcopy"
)

// cliHTTPRefusal keeps the daemon's wire values intact while a human-facing
// command chooses whether a verified catalog has a matching translation.
type cliHTTPRefusal struct {
	Code      string
	Detail    string
	DetailKey string
}

func parseCLIHTTPRefusal(data []byte) (cliHTTPRefusal, bool) {
	var nested contract.AuthRefusal
	if json.Unmarshal(data, &nested) == nil && nested.Error.Code != "" {
		return cliHTTPRefusal{nested.Error.Code, nested.Error.Message, nested.Error.DetailKey}, true
	}
	var flat contract.Refusal
	if json.Unmarshal(data, &flat) == nil && flat.Error != "" {
		return cliHTTPRefusal{flat.Error, flat.Detail, flat.DetailKey}, true
	}
	return cliHTTPRefusal{}, false
}

func (r cliHTTPRefusal) humanDetail(language string) string {
	return productcopy.HTTPRefusalText(language, r.DetailKey, r.Detail)
}

func humanHTTPAnswer(data []byte) string {
	if refusal, ok := parseCLIHTTPRefusal(data); ok {
		return refusal.Code + ": " + refusal.humanDetail(currentCLILanguage())
	}
	return strings.TrimSpace(string(data))
}
