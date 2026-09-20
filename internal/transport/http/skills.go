package http

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/skillmenu"
	"github.com/sainteye/clawdline-go/internal/adapters/transcript"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/capacity"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// skillsPath recognises GET /v1/sessions/{id}/skills and returns the id.
func skillsPath(r *http.Request) (string, bool) {
	return sessionVerbIs(r, "skills", http.MethodGet)
}

// sessionSkillsRoute answers the slash menu: the skills this particular
// session can invoke (skills.schema.json).
//
// **Read level.** It names commands and one line of description each; it
// types nothing, runs nothing a skill contains, and sends no path and no
// SKILL.md body, which is why the Swift app lets a paired phone read it.
//
// Claude's are read from its working directory, Codex's from the catalog in
// its own rollout — the key carries the assistant as well as the place,
// because one key for both would hand a Codex session Claude's skills, a wrong
// answer served quickly.
func (s *Server) sessionSkillsRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.ownsSessions() {
		s.forwardUpstream(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	item, err := s.actions().Find(ctx, id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	home, _ := os.UserHomeDir()
	var key string
	var read func() skillmenu.Reading
	switch item.Assistant {
	case session.AssistantClaude:
		// No working directory is this daemon not knowing where the session
		// is, the Swift route's 404, not a session with no skills.
		if strings.TrimSpace(item.CWD) == "" {
			writeRefusal(w, http.StatusNotFound, "not_found",
				"Could not find that session's working directory")
			return
		}
		key = "claude\x00" + item.CWD
		read = func() skillmenu.Reading { return skillmenu.Claude(item.CWD, home) }
	case session.AssistantCodex:
		// A session started fresh carries no thread id, so its rollout cannot
		// be named: an empty menu, as the Swift app answers it. The rollout is
		// looked for only on a miss — finding it walks ~/.codex/sessions.
		if item.ConversationID == "" {
			writeSkills(w, skillmenu.Reading{Skills: []skillmenu.Skill{}}, time.Now())
			return
		}
		key = "codex\x00" + item.ConversationID
		read = func() skillmenu.Reading {
			path := transcript.CodexPath(home, item.ConversationID)
			if path == "" {
				return skillmenu.Reading{Skills: []skillmenu.Skill{}}
			}
			return skillmenu.Codex(path, home)
		}
	default:
		writeSkills(w, skillmenu.Reading{Skills: []skillmenu.Skill{}}, time.Now())
		return
	}
	if s.skillMenu == nil {
		writeSkills(w, read(), time.Now())
		return
	}
	reading, at := s.skillMenu.Get(key, read)
	writeSkills(w, reading, at)
}

func writeSkills(w http.ResponseWriter, r skillmenu.Reading, at time.Time) {
	reply := contract.SkillsReply{
		Skills:     make([]contract.AssistantSkill, 0, len(r.Skills)),
		ObservedAt: float64(at.UnixMilli()) / 1000,
		Truncated:  r.Truncated,
	}
	for _, sk := range r.Skills {
		reply.Skills = append(reply.Skills, contract.AssistantSkill{
			Name:        sk.Name,
			Description: sk.Description,
			Source:      contract.SkillSource(sk.Source),
		})
	}
	writeJSON(w, reply)
}

// skillsReading is the `cache.session_skills` row. A server built without the
// cache has asked for nobody's skills, which is a known zero.
func (s *Server) skillsReading() capacity.Reading {
	if s.skillMenu == nil {
		return capacity.Reading{Known: true, Note: "nothing has asked for a session's skills"}
	}
	return s.skillMenu.Reading()
}
