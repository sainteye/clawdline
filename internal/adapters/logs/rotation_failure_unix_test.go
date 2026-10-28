//go:build unix

package logs

import (
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"os"
	"testing"
	"time"
)

func TestFailedRotationCannotBeClearedByAppendingToTheOldFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can rename files in a read-only directory")
	}
	w, _ := open(t)
	w.SetLimit(1)
	_, _ = w.Write([]byte("seed"))
	if err := os.Chmod(w.Dir(), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(w.Dir(), 0700) })
	_, _ = w.Write([]byte("still written to the old file"))
	r := w.Reading()
	if !r.Known || !r.Failing || r.Counters.WriteErrors == 0 {
		t.Fatal("successful append hid rotation failure", r)
	}
	var e capacity.Entry
	for _, row := range capacity.Register() {
		if row.Name == capacity.LogDaemon {
			e = row
		}
	}
	tr := capacity.NewTracker()
	st, _ := tr.Observe(capacity.Resolved{Entry: e, Limit: 1}, r, time.Now())
	if st.State != capacity.Full {
		t.Fatal("failed rotation did not alert", st)
	}
	if err := os.Chmod(w.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("recovered"))
	r = w.Reading()
	if r.Failing || r.Counters.Rotated != 1 {
		t.Fatal("successful rotation did not recover", r)
	}
	st, _ = tr.Observe(capacity.Resolved{Entry: e, Limit: 1}, r, time.Now())
	if st.State != capacity.OK {
		t.Fatal(st)
	}
}
