package agenthandoff

import "testing"

func TestTaskBodyClosesItsWireShapeBeforeAClaim(t *testing.T) {
	for _, raw := range []string{
		`{"assistant":"claude","title":"Disk inventory","instructions":"Inspect disk"}`,
		`{"assistant":"codex","title":"Disk inventory","instructions":"Inspect disk"}`,
		`{"assistant":"antigravity","title":"Disk inventory","instructions":"Inspect disk"}`,
	} {
		if _, err := ParseTaskBody([]byte(raw)); err != nil {
			t.Fatalf("valid task body %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"assistant":"shell","title":"Disk inventory","instructions":"Inspect disk"}`,
		`{"assistant":"claude","title":"Disk inventory","instructions":"Inspect disk","project_path":"/foreign"}`,
		`{"assistant":"claude","title":"Disk inventory","instructions":"Inspect disk"} {}`,
		"{\"assistant\":\"claude\",\"title\":\"Disk\\ncommand\",\"instructions\":\"Inspect disk\"}",
	} {
		if _, err := ParseTaskBody([]byte(raw)); err == nil {
			t.Fatalf("unsafe task body admitted: %s", raw)
		}
	}
}
