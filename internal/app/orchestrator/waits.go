package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/app/lane"
)

// File waits: one session holds paths in a repository, others wait for it to
// let them go (the Swift app's coordination waits, Orchestrator.swift
// "Cross-session coordination waits").
//
// The behaviour is the Swift app's: a wait is keyed by repository, paths,
// owner and release condition, so a second waiter on the same thing joins it;
// the owner is told once, in its own composer, and releases explicitly; every
// waiter is told of the release in its own composer; a waiter showing a menu
// is not typed into and stays pending; nothing releases a wait on a clock.
//
// Two things differ on purpose. Both sessions are named by their conversation
// ids (#36): the Swift routes took terminal ids here while the dispatch
// routes took conversation ids under the same key, and its own release looked
// waiters up under a key the record did not have (`waiter_session_id` for
// `sessionId`), so a release never found a channel. And the open waits have a
// ceiling (WaitsOpenLimit, the register's `waits.open`), which the Swift app's
// did not.

const (
	// WaitsOpenLimit is how many waits may be open at once on this machine.
	WaitsOpenLimit = 256
	waitPathsCap   = 200
	waitPathBytes  = 1024
	waitRepoBytes  = 4096
	waitTextBytes  = 1000
	waitCommitText = 200
)

// WaitRequest is the body of POST /v1/orchestrator/waits.
type WaitRequest struct {
	Repository       string
	Paths            []string
	Owner            string
	Waiter           string
	Reason           string
	ReleaseCondition string
}

// WaitOutcome is a registration's answer.
type WaitOutcome struct {
	Wait         store.WaitRow
	Deduplicated bool
}

func badWait(message string) Refusal {
	return refuse(http.StatusBadRequest, "bad_wait", message)
}

// normaliseWait checks a request and puts its paths in the one spelling a
// wait is keyed by: repository-relative, cleaned, sorted, without repeats.
func normaliseWait(req WaitRequest) (WaitRequest, error) {
	repo := strings.TrimSpace(req.Repository)
	if repo == "" || !filepath.IsAbs(repo) || len(repo) > waitRepoBytes {
		return req, badWait("repository must be an absolute path.")
	}
	// Both spellings of the repository: as given, and with its symlinks
	// resolved. An absolute path is inside it if it is inside either — on
	// macOS /tmp is /private/tmp, and a caller naming one of each is naming
	// one place.
	given := filepath.Clean(repo)
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	repo = filepath.Clean(repo)
	inside := func(p string) (string, bool) {
		for _, root := range []string{given, repo} {
			if rel, err := filepath.Rel(root, filepath.Clean(p)); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
				return rel, true
			}
		}
		return "", false
	}
	if len(req.Paths) == 0 || len(req.Paths) > waitPathsCap {
		return req, badWait("paths must name between 1 and 200 paths.")
	}
	seen := map[string]bool{}
	paths := []string{}
	for _, raw := range req.Paths {
		p := strings.TrimSpace(raw)
		if p == "" || len(p) > waitPathBytes {
			return req, badWait("every path is between 1 and 1024 bytes.")
		}
		if filepath.IsAbs(p) {
			rel, ok := inside(p)
			if !ok {
				return req, badWait("a path must be inside the repository: " + p)
			}
			p = rel
		}
		p = filepath.Clean(p)
		if p == "." || p == ".." || strings.HasPrefix(p, "../") {
			return req, badWait("a path must be inside the repository: " + raw)
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, s := range []string{req.Owner, req.Waiter} {
		if !isLowercaseUUID(s) {
			return req, sessionIsTerminal(s)
		}
	}
	if req.Owner == req.Waiter {
		return req, badWait("a session does not wait on itself.")
	}
	if strings.TrimSpace(req.Reason) == "" || len(req.Reason) > waitTextBytes {
		return req, badWait("reason is between 1 and 1000 bytes.")
	}
	if strings.TrimSpace(req.ReleaseCondition) == "" || len(req.ReleaseCondition) > waitTextBytes {
		return req, badWait("release_condition is between 1 and 1000 bytes.")
	}
	req.Repository, req.Paths = repo, paths
	return req, nil
}

func samePaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// waitDelivery is one line owed to one session about one wait.
type waitDelivery struct {
	Wait     string `json:"wait"`
	Audience string `json:"audience"` // owner or waiter
	// Session is the conversation the line is for; Waiter is the waiter row
	// the delivery stamps (the waiter itself for a release, the one that
	// asked for a request).
	Session string `json:"session"`
	Waiter  string `json:"waiter"`
	Wire    string `json:"wire"`
}

// RegisterWait records a wait, or joins one, and tells its owner.
func (b *Broker) RegisterWait(ctx context.Context, raw WaitRequest) (WaitOutcome, error) {
	req, err := normaliseWait(raw)
	if err != nil {
		return WaitOutcome{}, err
	}
	// The waiter is the one asking, and must be here to be told of the
	// release; the Swift app's 404 is kept.
	if _, err := b.terminalFor(ctx, req.Waiter, ""); err != nil {
		return WaitOutcome{}, refuse(http.StatusNotFound, "waiter_not_found",
			"No live session has that waiter conversation id.")
	}
	now := b.now()
	var out WaitOutcome
	ids, err := b.Store.DecideWaits(ctx, func(open []store.WaitRow) (store.WaitChange, error) {
		var change store.WaitChange
		var found *store.WaitRow
		for i := range open {
			w := open[i]
			if w.Repository == req.Repository && w.Owner == req.Owner &&
				w.ReleaseCondition == req.ReleaseCondition && samePaths(w.Paths, req.Paths) {
				found = &open[i]
				break
			}
		}
		if found == nil {
			if ceiling := b.openWaits(); len(open) >= ceiling {
				return change, refuseWith(http.StatusTooManyRequests, "waits_full",
					fmt.Sprintf("%d file waits are already open on this machine; nothing was recorded.", ceiling),
					map[string]any{"retry_after": 60, "limit": ceiling})
			}
			w := store.WaitRow{ID: uuidLike(), Repository: req.Repository, Paths: req.Paths, Owner: req.Owner,
				ReleaseCondition: req.ReleaseCondition, CreatedAt: now}
			change.Put = &w
			found = &w
		}
		change.WaitID = found.ID
		var mine *store.WaiterRow
		for i := range found.Waiters {
			if found.Waiters[i].Waiter == req.Waiter && found.Waiters[i].Open() {
				mine = &found.Waiters[i]
			}
		}
		if mine != nil {
			out.Deduplicated = true
		} else {
			row := store.WaiterRow{Waiter: req.Waiter, Reason: req.Reason, CreatedAt: now}
			change.PutWaiters = []store.WaiterRow{row}
			found.Waiters = append(found.Waiters, row)
			mine = &row
			payload, _ := json.Marshal(map[string]any{"wait": found.ID, "owner": req.Owner, "waiter": req.Waiter})
			change.Events = append(change.Events, store.Event{Kind: "wait.joined", Subject: found.ID, Payload: payload})
		}
		out.Wait = *found
		// The owner is told once per waiter; a resend of a delivered request
		// types nothing.
		if mine.RequestDeliveredAt.IsZero() {
			wire, err := b.waitRequestWire(*found, req.Waiter, mine.Reason)
			if err != nil {
				return change, err
			}
			body, _ := json.Marshal(waitDelivery{Wait: found.ID, Audience: "owner", Session: req.Owner,
				Waiter: req.Waiter, Wire: wire})
			change.Effects = append(change.Effects, store.Effect{Kind: EffectWaitDelivery, Subject: found.ID, Payload: body})
		}
		return change, nil
	})
	if err != nil {
		return WaitOutcome{}, storeError(err)
	}
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state == store.EffectDone {
			continue
		}
		out.Wait = b.reloadWait(ctx, out.Wait)
		if res.outcome == "owner_busy" {
			return out, refuseWith(http.StatusConflict, "owner_busy",
				"The owner is showing a menu; nothing was typed and the wait stays recorded, undelivered. Ask again once the menu is gone.",
				map[string]any{"wait_id": out.Wait.ID})
		}
		return out, refuseWith(http.StatusBadGateway, "request_delivery_failed",
			"The wait is recorded but its owner could not be told: "+res.outcome,
			map[string]any{"wait_id": out.Wait.ID})
	}
	out.Wait = b.reloadWait(ctx, out.Wait)
	return out, nil
}

func (b *Broker) reloadWait(ctx context.Context, w store.WaitRow) store.WaitRow {
	open, err := b.Store.OpenWaits(ctx)
	if err != nil {
		return w
	}
	for _, x := range open {
		if x.ID == w.ID {
			return x
		}
	}
	return w
}

// ReleaseWait is the owner letting the paths go. Every waiter still waiting
// is told in its own composer; one showing a menu is left pending, and the
// wait stays open until every waiter has been told or has left.
func (b *Broker) ReleaseWait(ctx context.Context, id, owner, commit, note string) (int, error) {
	if !isLowercaseUUID(owner) {
		return 0, sessionIsTerminal(owner)
	}
	if len(commit) > waitCommitText || len(note) > waitTextBytes {
		return 0, badWait("commit is at most 200 bytes and note at most 1000.")
	}
	total := 0
	ids, err := b.Store.DecideWaits(ctx, func(open []store.WaitRow) (store.WaitChange, error) {
		var change store.WaitChange
		for _, w := range open {
			if w.ID != id {
				continue
			}
			if w.Owner != owner {
				return change, refuse(http.StatusForbidden, "wrong_owner", "Only the wait's owner releases it.")
			}
			total = len(w.Waiters)
			next := w
			next.ReleaseCommit, next.ReleaseNote = commit, note
			change.Put = &next
			change.WaitID = w.ID
			pending := 0
			for _, x := range w.Waiters {
				if !x.Open() {
					continue
				}
				pending++
				wire, err := b.waitReleaseWire(w, commit, note)
				if err != nil {
					return change, err
				}
				body, _ := json.Marshal(waitDelivery{Wait: w.ID, Audience: "waiter", Session: x.Waiter,
					Waiter: x.Waiter, Wire: wire})
				change.Effects = append(change.Effects, store.Effect{Kind: EffectWaitDelivery, Subject: w.ID, Payload: body})
			}
			if pending == 0 {
				next.ReleasedAt = b.now()
			}
			payload, _ := json.Marshal(map[string]any{"wait": w.ID, "commit": commit, "pending": pending})
			change.Events = []store.Event{{Kind: "wait.release", Subject: w.ID, Payload: payload}}
			return change, nil
		}
		return change, refuse(http.StatusNotFound, "not_found", "No open wait has that id.")
	})
	if err != nil {
		return 0, storeError(err)
	}
	sent, pending := 0, 0
	for _, res := range b.runRecorded(ctx, ids) {
		if res.state == store.EffectDone {
			sent++
		} else {
			pending++
		}
	}
	if pending > 0 {
		return total, refuseWith(http.StatusBadGateway, "release_incomplete",
			"Some waiters could not be told (a menu on screen, or not on this machine); they stay pending. Release again to retry them.",
			map[string]any{"id": id, "sent": sent, "pending": pending})
	}
	return total, nil
}

// CancelWait takes one waiter out; the wait closes when its last waiter
// leaves.
func (b *Broker) CancelWait(ctx context.Context, id, waiter string) error {
	if !isLowercaseUUID(waiter) {
		return sessionIsTerminal(waiter)
	}
	_, err := b.Store.DecideWaits(ctx, func(open []store.WaitRow) (store.WaitChange, error) {
		var change store.WaitChange
		for _, w := range open {
			if w.ID != id {
				continue
			}
			left := 0
			var mine *store.WaiterRow
			for i := range w.Waiters {
				x := w.Waiters[i]
				if x.Waiter == waiter && x.Open() {
					mine = &x
					continue
				}
				if x.Open() {
					left++
				}
			}
			if mine == nil {
				return change, refuse(http.StatusForbidden, "not_waiter", "That session is not waiting on this wait.")
			}
			mine.CancelledAt = b.now()
			change.WaitID = w.ID
			change.PutWaiters = []store.WaiterRow{*mine}
			if left == 0 {
				closed := w
				closed.ReleasedAt = b.now()
				change.Put = &closed
			}
			payload, _ := json.Marshal(map[string]any{"wait": w.ID, "waiter": waiter})
			change.Events = []store.Event{{Kind: "wait.cancelled", Subject: w.ID, Payload: payload}}
			return change, nil
		}
		return change, refuse(http.StatusNotFound, "not_found", "No open wait has that id.")
	})
	if err != nil {
		return storeError(err)
	}
	return nil
}

// Waits is every open wait.
func (b *Broker) Waits(ctx context.Context) ([]store.WaitRow, error) {
	out, err := b.Store.OpenWaits(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	return out, nil
}

// waitRequestWire is the `file_wait_request` notice, one line.
func (b *Broker) waitRequestWire(w store.WaitRow, waiter, reason string) (string, error) {
	body := "[Clawdline file-wait] Repo: " + w.Repository + ". Exact paths: " + strings.Join(w.Paths, ", ") + ". " +
		"Waiter Clawdline session id: " + waiter + ". Reason: " + reason + ". " +
		"Release condition: " + w.ReleaseCondition + ". After the condition is met, release this " +
		"wait through Clawdline so every registered waiter is notified."
	return noticeLine(map[string]any{
		"protocol": NoticeProtocol, "version": NoticeVersion, "kind": "file_wait_request", "audience": "owner",
		"wait_id": w.ID, "repository": w.Repository, "paths": w.Paths, "waiter_session_id": waiter,
		"reason": reason, "release_condition": w.ReleaseCondition, "body": body,
	})
}

// waitReleaseWire is the `file_wait_release` notice, one line.
func (b *Broker) waitReleaseWire(w store.WaitRow, commit, note string) (string, error) {
	body := "[Clawdline file-wait release] Repo: " + w.Repository + ". Exact paths: " + strings.Join(w.Paths, ", ") + ". "
	if commit != "" {
		body += "Landed/released in commit " + commit + ". "
	} else {
		body += "The owner explicitly released these paths without a commit. "
	}
	if note != "" {
		body += "Note: " + note + ". "
	}
	body += "Re-check HEAD, status and diff before editing or integrating."
	obj := map[string]any{
		"protocol": NoticeProtocol, "version": NoticeVersion, "kind": "file_wait_release", "audience": "waiter",
		"wait_id": w.ID, "repository": w.Repository, "paths": w.Paths, "body": body,
	}
	if commit != "" {
		obj["commit"] = commit
	}
	if note != "" {
		obj["note"] = note
	}
	return noticeLine(obj)
}

func noticeLine(obj map[string]any) (string, error) {
	encoded, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(string(encoded), "\n\r") {
		return "", errors.New("the notice could not be encoded on one line")
	}
	return "<clawdline-notice>" + string(encoded) + "</clawdline-notice>", nil
}

// runWaitDelivery types one wait line into the session it is for, and stamps
// the waiter row it answers. It never types into a session showing a menu.
func runWaitDelivery(ctx context.Context, b *Broker, e store.Effect) effectResult {
	var d waitDelivery
	if err := json.Unmarshal(e.Payload, &d); err != nil {
		return effectResult{state: store.EffectFailed, outcome: "unreadable effect: " + err.Error()}
	}
	target, err := b.terminalFor(ctx, d.Session, "")
	if err != nil {
		return effectResult{state: store.EffectFailed, outcome: "session_not_found"}
	}
	if b.Choosing != nil && b.Choosing(ctx, target.ID) {
		outcome := "waiter_busy"
		if d.Audience == "owner" {
			outcome = "owner_busy"
		}
		return effectResult{state: store.EffectFailed, outcome: outcome}
	}
	if err := b.typeLine(ctx, target.ID, d.Wire); err != nil {
		var busy lane.Busy
		if errors.As(err, &busy) {
			return effectResult{state: store.EffectFailed, outcome: "terminal_busy"}
		}
		return effectResult{state: store.EffectFailed, outcome: err.Error()}
	}
	at := b.now()
	_, err = b.Store.DecideWaits(ctx, func(open []store.WaitRow) (store.WaitChange, error) {
		var change store.WaitChange
		for _, w := range open {
			if w.ID != d.Wait {
				continue
			}
			change.WaitID = w.ID
			stillOpen := 0
			for _, x := range w.Waiters {
				if x.Waiter == d.Waiter {
					if d.Audience == "owner" && x.RequestDeliveredAt.IsZero() {
						x.RequestDeliveredAt = at
						change.PutWaiters = append(change.PutWaiters, x)
					}
					if d.Audience == "waiter" && x.ReleaseDeliveredAt.IsZero() {
						x.ReleaseDeliveredAt = at
						change.PutWaiters = append(change.PutWaiters, x)
						continue
					}
				}
				if x.Open() {
					stillOpen++
				}
			}
			// The last waiter told of a release closes the wait.
			if d.Audience == "waiter" && stillOpen == 0 {
				closed := w
				closed.ReleasedAt = at
				change.Put = &closed
			}
		}
		return change, nil
	})
	payload, _ := json.Marshal(map[string]any{"wait": d.Wait, "audience": d.Audience, "terminal": target.ID})
	res := effectResult{state: store.EffectDone, outcome: "typed",
		events: []store.Event{{Kind: "wait.delivered", Subject: d.Wait, Payload: payload}}}
	if err != nil {
		res.outcome = "typed; the delivery could not be stamped: " + err.Error()
	}
	return res
}
