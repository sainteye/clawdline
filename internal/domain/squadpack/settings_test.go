package squadpack

import "testing"

func TestPrivateDocumentRejectsDuplicateAndMalformedOverridesBeforePreview(t *testing.T) {
	for _, tc := range []struct{ name, raw, code string }{
		{"duplicate row", `{"settings":[{"definition_id":"vendor.persona.writer","payload":{}},{"definition_id":"vendor.persona.writer","payload":{}}]}`, ErrPrivateScope},
		{"duplicate key", `{"settings":[{"definition_id":"vendor.persona.writer","payload":{"handbook":"a","handbook":"b"}}]}`, ErrManifestInvalid},
		{"unknown field", `{"settings":[{"definition_id":"vendor.persona.writer","payload":{"command":"run"}}]}`, ErrManifestInvalid},
		{"duplicate skill", `{"settings":[{"definition_id":"vendor.persona.writer","payload":{"skills":[{"id":"vendor.skill.a","version":"1.0.0","enabled":true},{"id":"vendor.skill.a","version":"1.0.0","enabled":false}]}}]}`, ErrPrivateScope},
		{"missing settings", `{}`, ErrManifestInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePrivateDocument([]byte(tc.raw))
			if got := refusalCode(t, err); got != tc.code {
				t.Fatalf("got %s, want %s", got, tc.code)
			}
		})
	}
}
