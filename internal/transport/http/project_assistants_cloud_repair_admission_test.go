package http

// A re-pair of the sender while its send is inside runtime capability
// admission (the matrix's D1) must refuse the in-flight request as
// stale_version before anything is written, as Service effects do. Before the
// runtime rechecked the grant revision it answered 200 with an accepted
// receipt under the replaced grant.

import (
	"testing"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestProjectAssistantCloudAccessDefectD1RePairDuringAdmission(t *testing.T) {
	m := newPAMatrix(t)
	m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
	before := m.grant(t, "guest-writer")
	repaired := 0
	m.p.setCaps(func() {
		m.p.setCaps(nil)
		repaired++
		m.pair(t, "guest-writer", adaptercloud.ScopeOperate, true)
	})
	a, inner := m.word(t, "guest-writer", "send", m.sendBody(t, "repair-at-admission", "Must not run under the old grant"))
	after := m.grant(t, "guest-writer")
	if before.AuthorizationRevision == after.AuthorizationRevision || before.PrincipalID != after.PrincipalID {
		t.Fatal("fixture did not change only the authorization revision")
	}
	sends, _ := m.p.counts()
	if repaired != 1 || a.OK() || paMatrixCode(a, inner) != string(pa.StaleVersion) || sends != 0 {
		t.Fatalf("D1 in-flight request survived a fresh re-pair: status=%d sends=%d %s", a.Status, sends, a.Payload)
	}
}
