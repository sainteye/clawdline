package main

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitTask is one task's answer, with an open completion notice when it has
// finished.
func waitTask(id, state, notice string) string {
	delivery := ""
	if notice != "" {
		delivery = `,"completion_delivery":{"notice_id":"` + notice + `","state":"delivered","attempts":1,"created_at":1}`
	}
	return `{"ok":true,"task":{"id":"` + id + `","title":"t ` + id + `","state":"` + state + `"` + delivery +
		`,"result":{"status":"` + state + `","summary":"did ` + id + `"}}}`
}

// fakeTasks is a daemon whose tasks move through states on each GET, and
// which records every ACK.
type fakeTasks struct {
	mu     sync.Mutex
	states map[string][]string // the answer to each successive GET; the last repeats
	acks   []string            // "<task id> <body>"
	ackErr string              // a refusal to answer every ACK with
	s      *standIn            // which has already read each request's body
}

func (f *fakeTasks) answer(r *http.Request) (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/v1/orchestrator/tasks/")
	if id, ok := strings.CutSuffix(path, "/completion/ack"); ok {
		seen := f.s.requests()
		f.acks = append(f.acks, id+" "+string(seen[len(seen)-1].Body))
		if f.ackErr != "" {
			return 409, f.ackErr
		}
		return 200, `{"ok":true,"acknowledged":true,"changed":true,"notice_id":"n"}`
	}
	seq, ok := f.states[path]
	if !ok {
		return 404, `{"error":{"code":"not_found","message":"No task named that."}}`
	}
	state := seq[0]
	if len(seq) > 1 {
		f.states[path] = seq[1:]
	}
	notice := ""
	if state != "briefed" {
		notice = "n-" + path
	}
	return 200, waitTask(path, state, notice)
}

// fakeClock advances only when the wait sleeps.
func fakeClock() (waitClock, *[]time.Duration) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	var slept []time.Duration
	return waitClock{now: func() time.Time { return now },
		sleep: func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }}, &slept
}

func runWait(t *testing.T, f *fakeTasks, args ...string) (int, string, string) {
	t.Helper()
	opts, err := waitArgs(args)
	if err != nil {
		t.Fatalf("waitArgs(%v): %v", args, err)
	}
	s, b := newStandIn(t, f.answer)
	f.s = s
	clock, _ := fakeClock()
	var out, errs bytes.Buffer
	code := waitTasks(&out, &errs, b, opts, clock)
	return code, out.String(), errs.String()
}

func TestTaskWaitExits(t *testing.T) {
	for _, c := range []struct {
		name   string
		states map[string][]string
		args   []string
		want   int
		says   []string
		acks   int
	}{
		{"all succeed", map[string][]string{"a": {"briefed", "success"}, "b": {"briefed", "briefed", "success"}},
			[]string{"a", "b"}, 0, []string{"did a", "did b", "notice:       closed (read)"}, 2},
		{"one failed", map[string][]string{"a": {"success"}, "b": {"briefed", "failure"}},
			[]string{"a", "b", "a"}, 1, []string{"did a", "did b"}, 2},
		{"timeout keeps what settled", map[string][]string{"a": {"success"}, "b": {"briefed"}},
			[]string{"--timeout", "30s", "a", "b"}, 3, []string{"did a", "still running: b  t b (briefed)"}, 1},
		{"any returns on the first", map[string][]string{"a": {"briefed"}, "b": {"briefed", "success"}},
			[]string{"a", "--any", "b"}, 0, []string{"did b", "still running: a"}, 1},
		{"any with nothing settled times out", map[string][]string{"a": {"briefed"}},
			[]string{"--any", "--timeout", "5s", "a"}, 3, []string{"still running: a"}, 0},
		{"unknown id", map[string][]string{"a": {"briefed"}},
			[]string{"a", "nope"}, 4, nil, 0},
		{"one cancelled", map[string][]string{"a": {"success"}, "b": {"briefed", "cancelled"}},
			[]string{"a", "b"}, 5, []string{"did a", "did b"}, 2},
		{"a failure is over a cancel", map[string][]string{"a": {"failure"}, "b": {"cancelled"}},
			[]string{"a", "b"}, 1, []string{"did a", "did b"}, 2},
		{"a timeout is over a cancel", map[string][]string{"a": {"cancelled"}, "b": {"briefed"}},
			[]string{"--timeout", "30s", "a", "b"}, 3, []string{"did a", "still running: b"}, 1},
		{"any on a cancel", map[string][]string{"a": {"briefed"}, "b": {"cancelled"}},
			[]string{"--any", "a", "b"}, 5, []string{"did b", "still running: a"}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeTasks{states: c.states}
			code, out, errs := runWait(t, f, c.args...)
			if code != c.want {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, c.want, out, errs)
			}
			for _, s := range c.says {
				if !strings.Contains(out, s) {
					t.Errorf("stdout does not say %q:\n%s", s, out)
				}
			}
			if len(f.acks) != c.acks {
				t.Errorf("%d ACKs, want %d: %v", len(f.acks), c.acks, f.acks)
			}
			for _, a := range f.acks {
				id, body, _ := strings.Cut(a, " ")
				if body != `{"notice_id":"n-`+id+`"}` {
					t.Errorf("ACK %q does not carry its own notice", a)
				}
			}
			if c.want == 4 && !strings.Contains(errs, "could not read task nope: refused, 404 not_found") {
				t.Errorf("stderr: %q", errs)
			}
			if c.want == 3 && !strings.Contains(errs, "timed out") {
				t.Errorf("stderr: %q", errs)
			}
		})
	}
}

// A daemon that does not answer is retried a bounded number of times, then
// said as unknown, not waited out or taken as success.
func TestTaskWaitSaysAnUnreachableDaemon(t *testing.T) {
	b := &broker{base: "http://127.0.0.1:1", token: thinToken, client: &http.Client{Timeout: time.Second}}
	clock, slept := fakeClock()
	var out, errs bytes.Buffer
	code := waitTasks(&out, &errs, b, waitOptions{ids: []string{"a"}, timeout: time.Hour}, clock)
	if code != 4 || !strings.Contains(errs.String(), "could not read task a: the daemon at") {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	if len(*slept) != waitReadLimit-1 {
		t.Fatalf("slept %d times before giving up, want %d", len(*slept), waitReadLimit-1)
	}
}

// The pause between polls backs off and never passes its bound.
func TestTaskWaitBacksOff(t *testing.T) {
	f := &fakeTasks{states: map[string][]string{"a": {"briefed"}}}
	opts, _ := waitArgs([]string{"--timeout", "2m", "a"})
	_, b := newStandIn(t, f.answer)
	clock, slept := fakeClock()
	var out, errs bytes.Buffer
	if code := waitTasks(&out, &errs, b, opts, clock); code != 3 {
		t.Fatalf("exit %d", code)
	}
	var total time.Duration
	for i, d := range *slept {
		total += d
		if d > waitPollLimit || (i > 0 && d < (*slept)[i-1] && i != len(*slept)-1) {
			t.Fatalf("pauses %v", *slept)
		}
	}
	if (*slept)[0] != waitFirstPoll || total != 2*time.Minute {
		t.Fatalf("pauses %v add up to %s", *slept, total)
	}
}

func TestTaskWaitArgs(t *testing.T) {
	got, err := waitArgs([]string{"a", "--any", "b", "a", "--timeout", "1m"})
	if err != nil || len(got.ids) != 2 || !got.any || got.timeout != time.Minute {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := waitArgs([]string{"a"}); got.timeout != 9*time.Minute {
		t.Fatalf("default timeout %s", got.timeout)
	}
	many := make([]string, waitIDLimit+1)
	for i := range many {
		many[i] = strings.Repeat("x", i+1)
	}
	for _, bad := range [][]string{nil, {"--timeout", "0s", "a"}, {"--timeout", "-1m", "a"},
		{"--timeout", "3h", "a"}, {"--bogus", "a"}, many} {
		if _, err := waitArgs(bad); err == nil {
			t.Errorf("waitArgs(%v) was accepted", bad)
		}
	}
}

// `task show` closes a finished task's notice once it has printed it, and
// leaves a running task's, an acknowledged one, and a refused ACK's exit alone.
func TestTaskShowClosesTheNotice(t *testing.T) {
	const id = taskTestID
	for _, c := range []struct {
		name, task, ackErr string
		json               bool
		acks               int
		says, warns        string
	}{
		{"finished", waitTask(id, "success", "n-1"), "", false, 1, "notice:       closed (read)", ""},
		{"finished, --json", waitTask(id, "failure", "n-1"), "", true, 1, `"completion_delivery"`, ""},
		{"running", waitTask(id, "briefed", ""), "", false, 0, "briefed", ""},
		{"running with a notice", strings.Replace(waitTask(id, "briefed", "n-1"), "delivered", "pending", 1),
			"", false, 0, "briefed", ""},
		{"already acknowledged", strings.Replace(waitTask(id, "success", "n-1"), `"delivered"`, `"acknowledged"`, 1),
			"", false, 0, "success", ""},
		{"ACK refused", waitTask(id, "success", "n-1"),
			`{"error":{"code":"completion_notice_mismatch","message":"nope"}}`, false, 1, "success",
			"clawdline task show: the completion notice n-1 of " + id +
				" was not closed: refused, completion_notice_mismatch: nope\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var acks []string
			var s *standIn
			s, b := newStandIn(t, func(r *http.Request) (int, string) {
				if r.Method == http.MethodPost {
					seen := s.requests()
					acks = append(acks, r.URL.Path+" "+string(seen[len(seen)-1].Body))
					if c.ackErr != "" {
						return 409, c.ackErr
					}
					return 200, `{"ok":true,"acknowledged":true,"changed":true,"notice_id":"n-1"}`
				}
				return 200, c.task
			})
			var out, errs bytes.Buffer
			if code := showTask(&out, &errs, b, id, c.json); code != 0 {
				t.Fatalf("exit %d: %s", code, errs.String())
			}
			if len(acks) != c.acks {
				t.Fatalf("%d ACKs, want %d: %v", len(acks), c.acks, acks)
			}
			if c.acks == 1 && acks[0] != "/v1/orchestrator/tasks/"+id+"/completion/ack "+`{"notice_id":"n-1"}` {
				t.Fatalf("ACK %q", acks[0])
			}
			if !strings.Contains(out.String(), c.says) {
				t.Errorf("stdout does not say %q:\n%s", c.says, out.String())
			}
			if c.ackErr != "" && strings.Contains(out.String(), "closed") {
				t.Errorf("a refused ACK was said as closed:\n%s", out.String())
			}
			if errs.String() != c.warns {
				t.Errorf("stderr %q, want %q", errs.String(), c.warns)
			}
		})
	}
}
