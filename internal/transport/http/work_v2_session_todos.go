package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"strings"
	"time"
)

type directTodoV2Wire struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	CreatedAt   int64  `json:"created_at"`
	SentAt      *int64 `json:"sent_at"`
	ReadAt      *int64 `json:"read_at"`
	CompletedAt *int64 `json:"completed_at"`
	CompletedBy string `json:"completed_by,omitempty"`
	// CreatedBy is who wrote the row: the person's actor, or the Session's own
	// conversation id when the Session added it on the person's request.
	CreatedBy string            `json:"created_by"`
	Version   int64             `json:"version"`
	Images    []workV2ImageWire `json:"images,omitempty"`
}

func directTodoWire(td work.DirectTodoV2, images []work.DirectTodoImageV2) directTodoV2Wire {
	out := directTodoV2Wire{ID: td.ID, Text: td.Text, CreatedAt: td.CreatedAt.Unix(), SentAt: optionalUnix(td.SentAt),
		ReadAt: optionalUnix(td.ReadAt), CompletedAt: optionalUnix(td.CompletedAt), CompletedBy: td.CompletedBy,
		CreatedBy: td.CreatedBy, Version: td.Version}
	for _, image := range images {
		out.Images = append(out.Images, workV2ImageWire{ID: image.ID, Title: image.Title, MediaType: image.MediaType,
			ByteCount: image.ByteCount, Width: image.Width, Height: image.Height, Position: image.Position,
			CreatedBy: image.CreatedBy, CreatedAt: image.CreatedAt.Unix()})
	}
	return out
}

func directTodoAnswer(td work.DirectTodoV2, images []work.DirectTodoImageV2) []byte {
	b, _ := json.Marshal(map[string]any{"ok": true, "todo": directTodoWire(td, images)})
	return b
}

const sessionTodoConversationPrefix = "conversation:"

func (s *Server) workV2SessionTodos(w http.ResponseWriter, r *http.Request, parts []string) {
	actor, ok := requirePersonWorkV2(w, r)
	if !ok {
		return
	}
	targetID := decodeSegment(parts[0])
	terminalID, conversation := targetID, ""
	var err error
	// The list row already carries the Session's durable conversation id. A
	// read of work keyed by that id must not depend on a fresh terminal scan:
	// Cloud may still be drawing an honestly retained, unverified row while an
	// iTerm or tmux inventory read is temporarily incomplete. Mutations keep
	// resolving the terminal below because Send and ownership-changing actions
	// need the live target they act on.
	if len(parts) == 1 && r.Method == http.MethodGet && strings.HasPrefix(targetID, sessionTodoConversationPrefix) {
		conversation = strings.TrimPrefix(targetID, sessionTodoConversationPrefix)
		if !workID(conversation) {
			writeRefusal(w, http.StatusBadRequest, "conversation_id_malformed", "The Session conversation id must be one lowercase UUID.")
			return
		}
	} else {
		find := s.actions().Find
		if len(parts) == 1 && r.Method == http.MethodGet {
			// A read of the to-do list only needs the conversation id the
			// row already showed (FindForRead); a mutation below still
			// resolves the live terminal it acts for.
			find = s.actions().FindForRead
		}
		sess, findErr := find(r.Context(), terminalID)
		if findErr != nil || sess.ConversationID == "" {
			writeRefusal(w, http.StatusConflict, "session_unavailable", "The Session is unavailable or has no conversation id.")
			return
		}
		conversation = sess.ConversationID
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.workV2SessionTodosRead(w, r, conversation, func() {
			rows, truncated, err := s.workV2().DirectTodos(r.Context(), conversation, true, false)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			items, itemTruncated, err := s.workV2().List(r.Context(), "", conversation, "open", "")
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			if err := s.store.ObserveWorkGateFeedback(r.Context(), conversation, time.Now().UTC().Truncate(time.Second)); err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			recent, recentTruncated, err := s.workV2().RecentlyCompleted(r.Context(), conversation)
			if err != nil {
				s.writeWorkV2Error(w, err)
				return
			}
			todos := make([]directTodoV2Wire, 0, len(rows))
			for _, td := range rows {
				images, imageErr := s.workV2().DirectTodoImages(r.Context(), td.ID)
				if imageErr != nil {
					s.writeWorkV2Error(w, imageErr)
					return
				}
				todos = append(todos, directTodoWire(td, images))
			}
			itemOf := func(v app.WorkV2View) workV2ItemWire { return s.workV2ItemOf(nil, v) }
			if len(items)+len(recent) > 0 {
				itemOf = s.workV2ItemProjector(r.Context())
			}
			assigned := make([]workV2ItemWire, 0, len(items))
			assignedIDs := make(map[string]bool, len(items))
			for _, item := range items {
				assigned = append(assigned, itemOf(item))
				assignedIDs[item.Item.ID] = true
			}
			decisions, decisionsError := readSessionTodoDecisions(r.Context(), assignedIDs, conversation, s.store.Decisions)
			completed := make([]workV2ItemWire, 0, len(recent))
			for _, item := range recent {
				completed = append(completed, itemOf(item))
			}
			writeJSON(w, map[string]any{"ok": true, "assigned_items": assigned, "recent_items": completed,
				"open_decisions": decisions, "decisions_error": decisionsError,
				"direct_todos": todos, "truncated": truncated || itemTruncated || recentTruncated})
		})
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPost {
		var body struct {
			Text string `json:"text"`
		}
		raw, ok := readWorkV2Body(w, r, &body)
		if !ok {
			return
		}
		k, ok := s.beginWorkV2Write(w, r, actor, raw)
		if !ok {
			return
		}
		var answer []byte
		_, err := s.workV2().CreateDirectTodo(r.Context(), app.NewDirectTodoV2{SessionID: conversation, Text: body.Text, Actor: actor},
			func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = directTodoAnswer(td, nil)
				return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
			})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) == 3 && parts[2] == "images" {
		if r.Method != http.MethodPost {
			writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A Session to-do reference image is added with POST.")
			return
		}
		var body struct {
			ExpectedVersion int64  `json:"expected_version"`
			Title           string `json:"title"`
			DataURL         string `json:"data_url"`
			Position        int64  `json:"position"`
		}
		raw, ok := readWorkV2BodyAtMost(w, r, &body, workV2ImageBodyLimit, "A reference-image request is at most 18 MiB.")
		if !ok {
			return
		}
		bytes, ok := artifacts.DecodeDataURL(body.DataURL)
		if !ok {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
			return
		}
		policy := artifacts.ProductionPolicy
		policy.MaxEncodedBytes = workV2ImageByteLimit
		normalized, normalizeErr := artifacts.Normalize(r.Context(), bytes, policy)
		if normalizeErr != nil {
			var refusal artifacts.Refusal
			if errors.As(normalizeErr, &refusal) {
				writeRefusal(w, refusal.Status, refusal.Code, refusal.Message)
			} else {
				writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_image", "Choose a supported raster image.")
			}
			return
		}
		k, ok := s.beginWorkV2Write(w, r, actor, raw)
		if !ok {
			return
		}
		var answer []byte
		_, _, err := s.workV2().AddDirectTodoImage(r.Context(), parts[1], app.AddDirectTodoImageV2{
			ExpectedVersion: body.ExpectedVersion, SessionID: conversation, Title: body.Title,
			Data: normalized.Data, MediaType: normalized.MediaType, Width: normalized.Width, Height: normalized.Height,
			Position: body.Position, Actor: actor,
		}, func(td work.DirectTodoV2, image work.DirectTodoImageV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
			answer = directTodoAnswer(td, []work.DirectTodoImageV2{image})
			return k, store.ReceiptAnswer{Status: http.StatusCreated, Body: answer}, true
		})
		if err != nil {
			_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
			s.writeWorkV2Error(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(answer)
		return
	}
	if len(parts) != 3 || r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET, POST, or a to-do action.")
		return
	}
	var empty struct{}
	raw, ok := readWorkV2Body(w, r, &empty)
	if !ok {
		return
	}
	k, ok := s.beginWorkV2Write(w, r, actor, raw)
	if !ok {
		return
	}
	id, action := parts[1], parts[2]
	var answer []byte
	switch action {
	case "send":
		rows, _, readErr := s.workV2().DirectTodos(r.Context(), conversation, true, false)
		if readErr != nil {
			err = readErr
			break
		}
		var found *work.DirectTodoV2
		for n := range rows {
			if rows[n].ID == id {
				found = &rows[n]
				break
			}
		}
		if found == nil {
			err = &app.WorkError{Status: http.StatusNotFound, Code: "todo_not_found", Message: "No such direct to-do belongs to this Session."}
			break
		}
		if err = app.CheckDirectTodoSend(*found, conversation, time.Now()); err != nil {
			break
		}
		pictures, pictureErr := s.store.DirectTodoV2ImagePayloads(r.Context(), found.ID)
		if refusal, named := referenceImageFileRefusal(pictureErr); named {
			err = refusal
			break
		}
		if pictureErr != nil {
			err = &app.WorkError{Status: http.StatusServiceUnavailable, Code: "store_unavailable", Message: pictureErr.Error()}
			break
		}
		dataURLs := make([]string, 0, len(pictures))
		for _, picture := range pictures {
			dataURLs = append(dataURLs, "data:"+picture.Image.MediaType+";base64,"+base64.StdEncoding.EncodeToString(picture.Data))
		}
		if _, sendErr := s.actions().SendWithPictures(r.Context(), terminalID,
			app.DirectTodoSendText(found.ID, found.Text), dataURLs); sendErr != nil {
			err = &app.WorkError{Status: http.StatusBadGateway, Code: "send_failed", Message: sendErr.Error()}
			break
		}
		var td work.DirectTodoV2
		td, err = s.workV2().MarkDirectTodoSent(r.Context(), id, conversation, time.Now())
		if err == nil {
			answer = directTodoAnswer(td, nil)
			_ = s.store.CompleteReceipt(r.Context(), k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer})
		}
	case "complete":
		_, err = s.workV2().CompleteDirectTodo(r.Context(), id, conversation, actor, true,
			func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = directTodoAnswer(td, nil)
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
	case "reopen":
		_, err = s.workV2().ReopenDirectTodo(r.Context(), id, conversation,
			func(td work.DirectTodoV2) (store.ReceiptKey, store.ReceiptAnswer, bool) {
				answer = directTodoAnswer(td, nil)
				return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
			})
	case "delete":
		answer, _ = json.Marshal(map[string]any{"ok": true, "deleted": id})
		err = s.workV2().DeleteDirectTodo(r.Context(), id, conversation, func() (store.ReceiptKey, store.ReceiptAnswer, bool) {
			return k, store.ReceiptAnswer{Status: http.StatusOK, Body: answer}, true
		})
	default:
		err = &app.WorkError{Status: http.StatusNotFound, Code: "action_not_found", Message: "No such to-do action."}
	}
	if err != nil {
		_ = s.store.ReleaseReceipt(context.WithoutCancel(r.Context()), k)
		s.writeWorkV2Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(answer)
}

// sessionTodoDecisionReadMillisecondsLimit bounds optional question projection.
const sessionTodoDecisionReadMillisecondsLimit = 300

// Decision context is optional. A stalled or failed lookup must leave the
// already projected todo list available with a typed missing-context hint.
func readSessionTodoDecisions(ctx context.Context, assignedIDs map[string]bool, conversation string,
	read func(context.Context, store.DecisionQuery) ([]work.Decision, error)) ([]decisionWire, *string) {
	if len(assignedIDs) == 0 {
		return []decisionWire{}, nil
	}
	decisionContext, cancel := context.WithTimeout(ctx, time.Duration(sessionTodoDecisionReadMillisecondsLimit)*time.Millisecond)
	defer cancel()
	rows, err := read(decisionContext, store.DecisionQuery{
		State: work.DecisionOpen, Session: conversation, Limit: app.WorkPageSize,
	})
	if err != nil {
		code := "decisions_unavailable"
		return []decisionWire{}, &code
	}
	if len(rows) == app.WorkPageSize {
		code := "decisions_truncated"
		return assignedOpenDecisions(rows, assignedIDs, conversation), &code
	}
	return assignedOpenDecisions(rows, assignedIDs, conversation), nil
}

func assignedOpenDecisions(rows []work.Decision, assignedIDs map[string]bool, conversation string) []decisionWire {
	out := make([]decisionWire, 0)
	for _, decision := range rows {
		if assignedIDs[decision.WorkID] && decision.Session == conversation && decision.State == work.DecisionOpen {
			out = append(out, decisionOf(decision))
		}
	}
	return out
}

// workV2SessionTodosRead chooses the cheap header operation before entering
// any full-detail dependency. The callback also keeps this boundary directly
// testable with stalled full-detail work.
func (s *Server) workV2SessionTodosRead(w http.ResponseWriter, r *http.Request, conversation string, full func()) {
	if values, present := r.URL.Query()["summary"]; present {
		if len(values) != 1 || values[0] != "1" {
			writeRefusal(w, http.StatusBadRequest, "invalid_summary", "Summary must be 1.")
			return
		}
		s.workV2SessionTodosSummary(w, r, conversation)
		return
	}
	full()
}

// workV2SessionTodosSummary uses only bounded rows needed for the folded
// header. In particular it never projects Projects or loads item relations,
// images, or gate feedback. Its counts have the same page bounds as the full
// Session answer, including the recently completed prefix.
func (s *Server) workV2SessionTodosSummary(w http.ResponseWriter, r *http.Request, conversation string) {
	todos, todoTruncated, err := s.workV2().DirectTodos(r.Context(), conversation, true, false)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	assigned, assignedTruncated, err := s.store.WorkV2Items(r.Context(), "", conversation, "open", "", app.WorkV2PageSize)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	recent, recentTruncated, err := s.store.CompletedWorkV2ForSession(r.Context(), conversation, app.WorkV2PageSize)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	done, active, waiting := len(recent), 0, 0
	for _, item := range assigned {
		switch item.Phase {
		case work.PhaseImplementing, work.PhaseVerifying, work.PhaseMerging, work.PhaseDeploying:
			active++
		default:
			waiting++
		}
	}
	for _, todo := range todos {
		if !todo.CompletedAt.IsZero() {
			done++
		} else if !todo.ReadAt.IsZero() || todo.CreatedBy == conversation {
			active++
		} else {
			waiting++
		}
	}
	writeJSON(w, map[string]any{"ok": true, "done": done, "active": active, "waiting": waiting,
		"truncated": todoTruncated || assignedTruncated || recentTruncated})
}
