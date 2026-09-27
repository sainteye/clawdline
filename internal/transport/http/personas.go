package http

import (
	"net/http"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/persona"
)

// personasRoute is GET /v1/personas: the built-in catalog a start, a resume, a
// dispatch or a Root Assignment may name (docs/personas.md). Read-level, like
// the place list it is picked beside. The texts stay on this machine; a page
// needs the names, the summaries and the bot, and nothing it draws is the
// definition itself.
func (s *Server) personasRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Personas are read with GET.")
		return
	}
	writeJSON(w, personaCatalog())
}

func personaCatalog() contract.PersonaCatalog {
	out := contract.PersonaCatalog{Personas: []contract.Persona{}, License: persona.UpstreamLicense}
	for _, p := range persona.All() {
		out.Personas = append(out.Personas, contract.Persona{
			ID:             p.ID,
			Teams:          p.Teams,
			Name:           contract.PersonaNames{En: p.Name.En, ZhHant: p.Name.ZhHant},
			Summary:        contract.PersonaNames{En: p.Summary.En, ZhHant: p.Summary.ZhHant},
			SuggestedKinds: append([]string{}, p.SuggestedKinds...),
			Icon:           contract.Icon{Accent: p.Icon.Accent, Cells: p.Icon.Cells},
			Source:         p.Source,
		})
	}
	return out
}
