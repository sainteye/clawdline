package squadpack

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// PrivateDocument carries overrides for exactly one explicitly named scope.
// Versions in the source machine are intentionally omitted; adoption compares
// target versions captured by its preview instead.
type PrivateDocument struct {
	Settings []PrivateSetting `json:"settings"`
}

type PrivateSetting struct {
	DefinitionID string          `json:"definition_id"`
	Payload      json.RawMessage `json:"payload"`
}

func ParsePrivateDocument(data []byte) (PrivateDocument, error) {
	if _, err := cloud.Parse(data); err != nil {
		return PrivateDocument{}, refuse(ErrManifestInvalid)
	}
	var doc PrivateDocument
	if err := strictJSON(data, &doc); err != nil || doc.Settings == nil {
		return PrivateDocument{}, refuse(ErrManifestInvalid)
	}
	seen := map[string]bool{}
	for _, row := range doc.Settings {
		if seen[row.DefinitionID] || row.DefinitionID == "" && len(row.Payload) == 0 {
			return PrivateDocument{}, refuse(ErrPrivateScope)
		}
		seen[row.DefinitionID] = true
		payload := bytes.TrimSpace(row.Payload)
		if len(payload) == 0 || payload[0] != '{' {
			return PrivateDocument{}, refuse(ErrManifestInvalid)
		}
		if row.DefinitionID == "" {
			var motion struct {
				Motion *bool `json:"motion"`
			}
			if err := strictJSON(row.Payload, &motion); err != nil {
				return PrivateDocument{}, refuse(ErrManifestInvalid)
			}
			continue
		}
		if !namespacePattern.MatchString(row.DefinitionID) {
			return PrivateDocument{}, refuse(ErrPrivateScope)
		}
		var override struct {
			Handbook   *string `json:"handbook"`
			AutoAssign *bool   `json:"auto_assign"`
			Skills     *[]struct {
				ID      string `json:"id"`
				Version string `json:"version"`
				Enabled bool   `json:"enabled"`
			} `json:"skills"`
		}
		if err := strictJSON(row.Payload, &override); err != nil {
			return PrivateDocument{}, refuse(ErrManifestInvalid)
		}
		if override.Skills != nil {
			refs := map[string]bool{}
			for _, ref := range *override.Skills {
				if !namespacePattern.MatchString(ref.ID) || ref.Version == "" || refs[ref.ID] {
					return PrivateDocument{}, refuse(ErrPrivateScope)
				}
				refs[ref.ID] = true
			}
		}
	}
	return doc, nil
}

func strictJSON(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return refuse(ErrManifestInvalid)
	}
	return nil
}
