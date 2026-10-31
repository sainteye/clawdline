package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/domain/work"
)

// Issue sync's link rows (docs/issue-sync.md, D73): one per opted-in item,
// holding where its issue lives, what the tracker was last seen to hold, and
// when the next call may be made. The item row is untouched; the reconciler
// reads the phase from it and these rows say the rest.

const workV2IssueLinkSchema = `
CREATE TABLE IF NOT EXISTS work_v2_issue_links (
  work_id         TEXT PRIMARY KEY REFERENCES work_v2_items(id) ON DELETE CASCADE,
  provider        TEXT NOT NULL,
  repo            TEXT NOT NULL DEFAULT '',
  jira_site       TEXT NOT NULL DEFAULT '',
  jira_project    TEXT NOT NULL DEFAULT '',
  jira_issue_type TEXT NOT NULL DEFAULT '',
  observed        TEXT NOT NULL DEFAULT '',
  external_key    TEXT NOT NULL DEFAULT '',
  url             TEXT NOT NULL DEFAULT '',
  last_error      TEXT NOT NULL DEFAULT '',
  attempts        INTEGER NOT NULL DEFAULT 0,
  next_attempt_at INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS work_v2_issue_links_due ON work_v2_issue_links(next_attempt_at);
`

// IssueLink is one item's link row.
type IssueLink struct {
	WorkID        string
	Target        work.IssueTarget
	Observed      work.IssueObserved
	ExternalKey   string
	URL           string
	LastError     string
	Attempts      int
	NextAttemptAt time.Time
	UpdatedAt     time.Time
}

// DueIssueLink is a link joined with what its item owes the tracker.
type DueIssueLink struct {
	Link        IssueLink
	Title       string
	Description string
	Phase       work.Phase
}

// ErrIssueLinkMoved refuses changing the target of a link whose issue
// already exists: the issue cannot follow it to another tracker.
var ErrIssueLinkMoved = errors.New("issue_sync_already_linked")

const issueLinkColumns = `l.work_id, l.provider, l.repo, l.jira_site, l.jira_project, l.jira_issue_type, l.observed,
  l.external_key, l.url, l.last_error, l.attempts, l.next_attempt_at, l.updated_at`

func scanIssueLink(row interface{ Scan(...any) error }, extra ...any) (IssueLink, error) {
	var l IssueLink
	var provider, observed string
	var next, updated int64
	dest := append([]any{&l.WorkID, &provider, &l.Target.Repo, &l.Target.JiraSite, &l.Target.JiraProject,
		&l.Target.JiraIssueType, &observed, &l.ExternalKey, &l.URL, &l.LastError, &l.Attempts, &next, &updated}, extra...)
	if err := row.Scan(dest...); err != nil {
		return IssueLink{}, err
	}
	l.Target.Provider = work.IssueProvider(provider)
	l.Observed = work.IssueObserved(observed)
	if next > 0 {
		l.NextAttemptAt = time.Unix(next, 0)
	}
	l.UpdatedAt = time.Unix(updated, 0)
	return l, nil
}

// LinkIssue opts an item into sync. Setting the same target again is a
// no-op; changing it is allowed only while no issue exists yet.
func (t *WorkV2Tx) LinkIssue(item work.ItemV2, target work.IssueTarget, actor string, at time.Time) error {
	if err := target.Validate(); err != nil {
		return err
	}
	prev, err := scanIssueLink(t.tx.QueryRowContext(t.ctx, `SELECT `+issueLinkColumns+` FROM work_v2_issue_links l WHERE l.work_id = ?`, item.ID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO work_v2_issue_links
		  (work_id, provider, repo, jira_site, jira_project, jira_issue_type, created_at, updated_at)
		  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, string(target.Provider), target.Repo, target.JiraSite,
			target.JiraProject, target.JiraIssueType, at.Unix(), at.Unix()); err != nil {
			return err
		}
	case err != nil:
		return err
	case prev.Target == target:
		return nil
	case prev.Observed != work.IssueNotCreated:
		return ErrIssueLinkMoved
	default:
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_issue_links SET provider = ?, repo = ?, jira_site = ?,
		  jira_project = ?, jira_issue_type = ?, last_error = '', attempts = 0, next_attempt_at = 0, updated_at = ?
		  WHERE work_id = ?`, string(target.Provider), target.Repo, target.JiraSite, target.JiraProject,
			target.JiraIssueType, at.Unix(), item.ID); err != nil {
			return err
		}
	}
	t.wrote++
	payload, _ := json.Marshal(map[string]string{"provider": string(target.Provider), "repo": target.Repo,
		"jira_site": target.JiraSite, "jira_project": target.JiraProject})
	return t.AppendEvent(work.EventV2{WorkID: item.ID, Kind: "issue.linked", Actor: actor,
		PreviousVersion: item.Version, NextVersion: item.Version, Payload: string(payload), At: at})
}

// IssueLinks answers the links of the given items, by item id.
func (s *Store) IssueLinks(ctx context.Context, ids []string) (map[string]IssueLink, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	out := map[string]IssueLink{}
	for start := 0; start < len(ids); start += 200 {
		end := min(start+200, len(ids))
		chunk := ids[start:end]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.rd.QueryContext(ctx, `SELECT `+issueLinkColumns+` FROM work_v2_issue_links l
		  WHERE l.work_id IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			l, err := scanIssueLink(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			out[l.WorkID] = l
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PendingIssueLinks answers the links due now whose item phase owes the
// tracker a call, by the rule of work.NextIssueOp written as SQL so a limit
// never hides a due link behind rows that owe nothing.
func (s *Store) PendingIssueLinks(ctx context.Context, now time.Time, limit int) ([]DueIssueLink, error) {
	if err := reading(); err != nil {
		return nil, err
	}
	rows, err := s.rd.QueryContext(ctx, `SELECT `+issueLinkColumns+`, i.title, i.description, i.phase
	  FROM work_v2_issue_links l JOIN work_v2_items i ON i.id = l.work_id
	  WHERE l.next_attempt_at <= ? AND (
	    (l.observed = '' AND i.phase IN ('implementing', 'verifying', 'merging', 'deploying')) OR
	    (l.observed = 'open' AND i.phase IN ('done', 'cancelled')) OR
	    (l.observed = 'closed' AND i.phase NOT IN ('done', 'cancelled')))
	  ORDER BY l.next_attempt_at, l.work_id LIMIT ?`, now.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueIssueLink
	for rows.Next() {
		var d DueIssueLink
		var phase string
		l, err := scanIssueLink(rows, &d.Title, &d.Description, &phase)
		if err != nil {
			return nil, err
		}
		d.Link, d.Phase = l, work.Phase(phase)
		out = append(out, d)
	}
	return out, rows.Err()
}

// RecordIssueSynced stores what a call established and writes an item event
// when the tracker's state changed.
func (s *Store) RecordIssueSynced(ctx context.Context, id string, observed work.IssueObserved, key, url string, at time.Time) error {
	return s.WriteWorkV2(ctx, func(t *WorkV2Tx) error {
		var prev string
		var version int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT l.observed, i.version FROM work_v2_issue_links l
		  JOIN work_v2_items i ON i.id = l.work_id WHERE l.work_id = ?`, id).Scan(&prev, &version); err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_issue_links SET observed = ?, external_key = ?, url = ?,
		  last_error = '', attempts = 0, next_attempt_at = 0, updated_at = ? WHERE work_id = ?`,
			string(observed), key, url, at.Unix(), id); err != nil {
			return err
		}
		t.wrote++
		if prev == string(observed) {
			return nil
		}
		payload, _ := json.Marshal(map[string]string{"observed": string(observed), "external_key": key, "url": url})
		return t.AppendEvent(work.EventV2{WorkID: id, Kind: "issue.synced", Actor: "daemon",
			PreviousVersion: version, NextVersion: version, Payload: string(payload), At: at})
	})
}

// RecordIssueFailed stores a failed call and when to try again. An item event
// is written only when the failure's code changes, so a link retried for an
// hour does not fill the item's history.
func (s *Store) RecordIssueFailed(ctx context.Context, id, code string, attempts int, next, at time.Time) error {
	return s.WriteWorkV2(ctx, func(t *WorkV2Tx) error {
		var prev string
		var version int64
		if err := t.tx.QueryRowContext(t.ctx, `SELECT l.last_error, i.version FROM work_v2_issue_links l
		  JOIN work_v2_items i ON i.id = l.work_id WHERE l.work_id = ?`, id).Scan(&prev, &version); err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_issue_links SET last_error = ?, attempts = ?,
		  next_attempt_at = ?, updated_at = ? WHERE work_id = ?`, code, attempts, next.Unix(), at.Unix(), id); err != nil {
			return err
		}
		t.wrote++
		if prev == code {
			return nil
		}
		payload, _ := json.Marshal(map[string]string{"error": code})
		return t.AppendEvent(work.EventV2{WorkID: id, Kind: "issue.failed", Actor: "daemon",
			PreviousVersion: version, NextVersion: version, Payload: string(payload), At: at})
	})
}

// RetryIssueLink makes a failed link due now; the person's Retry.
func (s *Store) RetryIssueLink(ctx context.Context, id string, at time.Time) (bool, error) {
	found := false
	err := s.WriteWorkV2(ctx, func(t *WorkV2Tx) error {
		res, err := t.tx.ExecContext(t.ctx, `UPDATE work_v2_issue_links SET next_attempt_at = 0, updated_at = ?
		  WHERE work_id = ?`, at.Unix(), id)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		found = n > 0
		t.wrote += n
		return nil
	})
	return found, err
}
