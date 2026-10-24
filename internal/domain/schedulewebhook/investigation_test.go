package schedulewebhook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func investigationFixture(t *testing.T) ([]byte, InvestigationRef, Identity) {
	t.Helper()
	raw, err := os.ReadFile("testdata/investigation-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var b InvestigationBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return raw, b.InvestigationRef, Identity{AccountID: "account_fixture", MachineID: b.Scope.MachineID}
}

func TestInvestigationFrozenBundleWire(t *testing.T) {
	raw, ref, identity := investigationFixture(t)
	if err := ValidateInvestigationBundle(raw, ref, identity); err != nil {
		t.Fatal(err)
	}
	at := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), MaxInvestigationBundleBytes-len(raw))...)
	if err := ValidateInvestigationBundle(at, ref, identity); err != nil {
		t.Fatal("refused exact bundle byte boundary", err)
	}
	if err := ValidateInvestigationBundle(append(at, ' '), ref, identity); err == nil {
		t.Fatal("accepted bundle beyond byte boundary")
	}

	for _, name := range []string{"hash", "bundle_content", "scope_group", "revision", "investigation", "machine", "scope", "trailing", "bytes", "float", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Clone(raw)
			r := ref
			id := identity
			switch name {
			case "hash":
				r.ContentHash = "sha256:" + strings.Repeat("0", 64)
			case "bundle_content":
				changed = bytes.Replace(changed, []byte(`"event": "timeout"`), []byte(`"event": "recovered"`), 1)
			case "scope_group":
				changed = bytes.Replace(changed, []byte(`"correlation": "fixture-operation"`), []byte(`"correlation": "other-operation"`), 1)
			case "revision":
				r.Revision++
			case "investigation":
				r.InvestigationID = "hiv_" + strings.Repeat("f", 32)
			case "machine":
				changed = bytes.Replace(changed, []byte(`"machine_id": "mac_fixture"`), []byte(`"machine_id": "svc_api"`), 1)
			case "scope":
				changed = bytes.Replace(changed, []byte(`"source_id": "fixture-source"`), []byte(`"source_id": "other-source"`), 1)
			case "trailing":
				changed = append(changed, []byte(` {}`)...)
			case "bytes":
				changed = append(changed, bytes.Repeat([]byte(" "), MaxInvestigationBundleBytes)...)
			case "float":
				changed = bytes.ReplaceAll(changed, []byte(`"revision": 3`), []byte(`"revision": 3.5`))
			case "duplicate":
				changed = bytes.Replace(changed, []byte(`"manifest": {`), []byte(`"manifest": {}, "manifest": {`), 1)
			}
			if err := ValidateInvestigationBundle(changed, r, id); err == nil {
				t.Fatal("accepted changed frozen evidence")
			}
		})
	}
}

func TestInvestigationReportClosedWire(t *testing.T) {
	raw := []byte(`{"schema":"clawdline.health_investigation.report.v1","investigation_id":"hiv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","conclusion":"fix_prepared","fix_commit":null,"evidence_refs":[]}`)
	if _, err := ParseInvestigationReport(raw); err != nil {
		t.Fatal(err)
	}
	at := append(bytes.Repeat([]byte(" "), MaxInvestigationReportBytes-len(raw)), raw...)
	if _, err := ParseInvestigationReport(at); err != nil {
		t.Fatal("refused exact report byte boundary", err)
	}
	if _, err := ParseInvestigationReport(append(at, ' ')); err == nil {
		t.Fatal("accepted report beyond byte boundary")
	}

	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"fix_prepared"`), []byte(`"recovered"`), 1),
		bytes.Replace(raw, []byte(`"fix_commit":null`), []byte(`"fix_commit":"/tmp/private"`), 1),
		bytes.Replace(raw, []byte(`"evidence_refs":[]`), []byte(`"evidence_refs":null`), 1),
		bytes.Replace(raw, []byte(`"evidence_refs":[]`), []byte(`"evidence_refs":[],"text":"secret"`), 1),
		append(bytes.Repeat([]byte(" "), MaxInvestigationReportBytes), raw...),
	} {
		if _, err := ParseInvestigationReport(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	refs := []string{}
	for i := 0; i < MaxInvestigationReportRefs; i++ {
		refs = append(refs, fmt.Sprintf("he_%040x", i+1))
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["evidence_refs"] = refs
	withRefs, _ := json.Marshal(m)
	if _, err := ParseInvestigationReport(withRefs); err != nil {
		t.Fatal(err)
	}
	m["evidence_refs"] = append(refs, "he_"+strings.Repeat("f", 40))
	over, _ := json.Marshal(m)
	if _, err := ParseInvestigationReport(over); err == nil {
		t.Fatal("accepted ninth or duplicate ref")
	}
}

func TestInvestigationCanonicalJSONUsesCloudEncoding(t *testing.T) {
	raw := []byte("{\"z\":\"<>&\\u2028\",\"a\":[1,true,null],\"😀\":2,\"\\ue000\":3}")
	got, err := CanonicalInvestigationJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":[1,true,null],\"z\":\"<>&\u2028\",\"😀\":2,\"\ue000\":3}"
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	for _, bad := range []string{`{"x":1,"x":2}`, `{"x":1.5}`, `{"x":9007199254740992}`} {
		if _, err := CanonicalInvestigationJSON([]byte(bad)); err == nil {
			t.Fatal("accepted ambiguous canonical JSON")
		}
	}
}

// serviceSourcedBundle is the shape Cloud actually serves for a Cloud service
// incident: the evidence comes from svc_api while the delivery is leased to,
// and its digest binds, the executing machine.
func serviceSourcedBundle(t *testing.T) ([]byte, InvestigationRef, Identity) {
	t.Helper()
	raw, ref, identity := investigationFixture(t)
	raw = bytes.ReplaceAll(raw, []byte(`"machine_id": "mac_fixture"`), []byte(`"machine_id": "svc_api"`))
	var b InvestigationBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalInvestigationJSON(b.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	fresh := "sha256:" + hex.EncodeToString(sum[:])
	raw = bytes.Replace(raw, []byte(ref.ContentHash), []byte(fresh), 1)
	ref.ContentHash = fresh
	return raw, ref, identity
}

func TestInvestigationServiceEvidenceRunsOnTheLeasedMachine(t *testing.T) {
	raw, ref, identity := serviceSourcedBundle(t)
	if identity.MachineID == "svc_api" {
		t.Fatal("fixture identity must be the executing machine, not the evidence source")
	}
	if err := ValidateInvestigationBundle(raw, ref, identity); err != nil {
		t.Fatalf("refused service-sourced evidence delivered to this machine: %v", err)
	}
}

func TestInvestigationRefusalNamesStageAndFieldOnly(t *testing.T) {
	raw, ref, identity := serviceSourcedBundle(t)
	cases := []struct {
		name, want string
		edit       func([]byte, *InvestigationRef) []byte
	}{
		{"schema", "envelope.schema", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.Replace(b, []byte(`clawdline.health_investigation.bundle.v1`), []byte(`clawdline.health_investigation.bundle.v0`), 1)
		}},
		{"hash_basis", "envelope.hash_basis", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.Replace(b, []byte("sha256 over key-sorted JSON of `bundle`"), []byte("sha256 over bundle"), 1)
		}},
		{"ref", "envelope.investigation_id", func(b []byte, r *InvestigationRef) []byte {
			r.InvestigationID = "hiv_" + strings.Repeat("f", 32)
			return b
		}},
		{"source", "envelope.scope.machine_id", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.Replace(b, []byte(`"machine_id": "svc_api"`), []byte(`"machine_id": ""`), 1)
		}},
		{"manifest", "manifest.scope.source_id", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.Replace(b, []byte(`"source_id": "fixture-source"`), []byte(`"source_id": "other-source"`), 1)
		}},
		{"group", "manifest.scope.group", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.Replace(b, []byte(`"correlation": "fixture-operation"`), []byte(`"correlation": "other-operation"`), 1)
		}},
		{"hash", "bundle.content_hash", func(b []byte, r *InvestigationRef) []byte {
			fake := "sha256:" + strings.Repeat("0", 64)
			b = bytes.Replace(b, []byte(r.ContentHash), []byte(fake), 1)
			r.ContentHash = fake
			return b
		}},
		{"number", "json", func(b []byte, _ *InvestigationRef) []byte {
			return bytes.ReplaceAll(b, []byte(`"revision": 3`), []byte(`"revision": 3.5`))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ref
			err := ValidateInvestigationBundle(c.edit(bytes.Clone(raw), &r), r, identity)
			var refusal *InvestigationRefusal
			if !errors.Is(err, ErrInvestigationInvalid) || !errors.As(err, &refusal) || refusal.Field != c.want {
				t.Fatalf("got %v, want a refusal naming %s", err, c.want)
			}
			if strings.Contains(err.Error(), "svc_api") || strings.Contains(err.Error(), "fixture") || strings.Contains(err.Error(), ref.ContentHash) {
				t.Fatalf("refusal leaked a value: %v", err)
			}
		})
	}
}
