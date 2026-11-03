//go:build darwin || linux

package process

import "testing"

func TestWorkflowIdentityRequiresTheProvidersOwnAncestor(t *testing.T) {
	const id = "abcdefghijklmnopqrstuv"
	table := "ttys001 10 10 10 1 sh -c codex; status=$?; exit $status clawdline-workflow:" + id + "\n" +
		"ttys001 11 10 10 10 node /bin/codex\n" +
		"ttys001 12 10 10 11 /vendor/codex\n" +
		"ttys002 13 13 13 1 /vendor/codex prompt mentions clawdline-workflow:" + id + "\n"
	rows := workflowParentTable(table)
	if got := workflowAncestor(rows, 12); got != id {
		t.Fatalf("provider ancestry not bound: %q", got)
	}
	if got := workflowAncestor(rows, 13); got != "" {
		t.Fatalf("prompt forged binding: %q", got)
	}
	row := rows[10]
	row.tty = "ttys002"
	rows[10] = row
	if got := workflowAncestor(rows, 12); got != "" {
		t.Fatalf("cross-terminal ancestor accepted: %q", got)
	}
	row.tty = "ttys001"
	row.group = "99"
	rows[10] = row
	if got := workflowAncestor(rows, 12); got != "" {
		t.Fatalf("cross-group ancestor accepted: %q", got)
	}
	rows[11] = workflowParent{parent: 12, tty: "ttys001", group: "10"}
	if got := workflowAncestor(rows, 12); got != "" {
		t.Fatalf("cycle accepted: %q", got)
	}
}
