package http

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/session"
)

const (
	// namingTailLimit is how many of the newest transcript entries are read
	// for the later requests and the latest reply. It is a count of parsed
	// rows; the bytes that reach the model are namingContextLimit.
	namingTailLimit = 400
	// namingContextLimit is what one smart-title turn reads, 12 KiB. The
	// first request alone (4 KiB) was not enough to name most sessions.
	namingContextLimit = 12 << 10
)

// namingContext is what a smart title is written from. The first request
// alone was measured to be the wrong input: most sessions here open with a
// one-line Clawdline wrapper ("read ASSIGNMENT.md", "read CHILD.md"), so a
// model given only that line names the procedure — "read the task brief" —
// whatever the model. The wrapper is resolved to the objective it points at,
// and the later requests and the latest reply say what the work became.
type namingContext struct {
	First     string
	Later     []string
	LastReply string
}

func (s *Server) sessionTail(item session.Session) (transcript.Page, error) {
	if s.sessionTailRead != nil {
		return s.sessionTailRead(item)
	}
	path := recordPath(item)
	if path == "" {
		return transcript.Page{}, transcript.ErrNoRecord
	}
	if item.Assistant == "codex" {
		return transcript.ReadCodex(path, namingTailLimit)
	}
	return transcript.ReadClaude(path, namingTailLimit)
}

// gatherNamingContext never fails on the tail: a session whose newest entries
// cannot be read is still named from its opening request.
func (s *Server) gatherNamingContext(ctx context.Context, item session.Session, first string) namingContext {
	out := namingContext{First: s.resolveWrapper(ctx, first)}
	page, err := s.sessionTail(item)
	if err != nil {
		return out
	}
	skippedFirst := false
	for _, entry := range page.Entries {
		text := strings.TrimSpace(entry.Text)
		if text == "" {
			continue
		}
		switch entry.Kind {
		case transcript.KindUser:
			// The tail reaches the start of a short conversation; the opening
			// request is already First.
			if !skippedFirst && page.Unread == 0 && text == strings.TrimSpace(first) {
				skippedFirst = true
				continue
			}
			out.Later = append(out.Later, text)
		case transcript.KindAssistant:
			out.LastReply = text
		}
	}
	return out
}

var (
	rootAssignmentID = regexp.MustCompile(`^You are an independently owned Clawdline Feature Root for Root Assignment ([0-9a-fA-F-]{36})`)
	childTaskID      = regexp.MustCompile(`^You are a Clawdline CHILD agent for task ([0-9a-fA-F-]{36})`)
	taskSecretWord   = regexp.MustCompile(`TASK_SECRET=\S+`)
	machineTag       = regexp.MustCompile(`(?s)<(system-reminder|task-notification)>.*?</(system-reminder|task-notification)>`)
)

// resolveWrapper turns a Clawdline launch line into what it launched. Only
// this daemon's own records are read — the assignment and the task it wrote —
// never a path the line names. A line that cannot be resolved is kept as it is.
func (s *Server) resolveWrapper(ctx context.Context, first string) string {
	text := strings.TrimSpace(first)
	if s.broker == nil {
		return text
	}
	if m := rootAssignmentID.FindStringSubmatch(text); m != nil {
		if a, err := s.broker.RootAssignmentByID(ctx, m[1]); err == nil {
			var sb strings.Builder
			sb.WriteString("A feature assignment.")
			if label := strings.TrimSpace(a.Label); label != "" {
				sb.WriteString("\nLabel: " + label)
			}
			sb.WriteString("\nObjective: " + strings.TrimSpace(a.Assignment.Objective))
			sb.WriteString("\nScope: " + strings.TrimSpace(a.Assignment.Scope))
			return sb.String()
		}
	}
	if m := childTaskID.FindStringSubmatch(text); m != nil {
		if r, _, err := s.broker.Record(ctx, m[1]); err == nil {
			return "A delegated task: " + strings.TrimSpace(r.Title) + "\n" + strings.TrimSpace(r.Instructions)
		}
	}
	return text
}

// namingText is the one block the model reads, within NamingContextBytes:
// a third for the opening request, the rest for the later requests (newest
// kept first when they do not all fit) and the latest reply. Secrets and
// machine-inserted blocks are removed before anything is measured.
func namingText(c namingContext) string {
	limit := int(CapacityLimit(capacity.NamingContextBytes))
	perRequest := limit / 10
	replyShare := limit / 8

	first := capBytes(scrub(c.First), limit/3)
	reply := capBytes(scrub(c.LastReply), replyShare)
	left := limit - len(first) - len(reply)

	var kept []string
	for i := len(c.Later) - 1; i >= 0; i-- {
		text := capBytes(scrub(c.Later[i]), perRequest)
		if text == "" {
			continue
		}
		// Each kept request also costs its separator.
		if len(text)+len(laterSeparator) > left {
			break
		}
		left -= len(text) + len(laterSeparator)
		kept = append(kept, text)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	var sb strings.Builder
	sb.WriteString("<opening_request>\n" + first + "\n</opening_request>")
	if len(kept) > 0 {
		sb.WriteString("\n\n<later_requests>\n" + strings.Join(kept, laterSeparator) + "\n</later_requests>")
	}
	if reply != "" {
		sb.WriteString("\n\n<latest_reply>\n" + reply + "\n</latest_reply>")
	}
	return sb.String()
}

const laterSeparator = "\n---\n"

func scrub(text string) string {
	text = machineTag.ReplaceAllString(text, "")
	text = taskSecretWord.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func capBytes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	text = text[:limit]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return strings.TrimSpace(text)
}
