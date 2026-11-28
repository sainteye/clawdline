package http

// A re-pair of the sender while its send is inside runtime capability
// admission (the matrix's D1) must refuse the in-flight request before
// anything is written. A new key revokes the old request's proof, and a new
// authorization revision for the same key makes the request stale.

import (
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantCloudAccessDefectD1RePairDuringAdmission(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rotateKey bool
		want      pa.ErrorCode
	}{
		{name: "same key", want: pa.StaleVersion},
		{name: "rotated key", rotateKey: true, want: pa.Revoked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newPAMatrix(t)
			m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
			before := m.grant(t, "guest-writer")
			repaired := 0
			m.p.setCaps(func() {
				m.p.setCaps(nil)
				repaired++
				if tc.rotateKey {
					m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
					return
				}
				pin := m.pins["guest-writer"]
				pin.PairedAt++
				if err := m.pinned().Pin(m.identity.AccountID, pin); err != nil {
					t.Fatal(err)
				}
				m.pins["guest-writer"] = pin
			})
			a, inner := m.word(t, "guest-writer", "send", m.sendBody(t, "repair-at-admission", "Must not run under the old grant"))
			after := m.grant(t, "guest-writer")
			if before.AuthorizationRevision == after.AuthorizationRevision || before.PrincipalID != after.PrincipalID {
				t.Fatal("fixture did not update the authorization for the same principal")
			}
			sends, _ := m.p.counts()
			if repaired != 1 || a.OK() || paMatrixCode(a, inner) != string(tc.want) || sends != 0 {
				t.Fatalf("D1 expected %s and no sends after re-pair: status=%d sends=%d %s", tc.want, a.Status, sends, a.Payload)
			}
		})
	}
}
