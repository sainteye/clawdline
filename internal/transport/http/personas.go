package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

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
	view, version := r.URL.Query().Get("view"), r.URL.Query().Get("version")
	if (view != "" && view != "display-v1") || (version != "" && (view == "" || !personaCatalogVersion(version))) {
		writeRefusal(w, http.StatusBadRequest, "invalid_persona_view", "Use display-v1 and a SHA-256 catalog version.")
		return
	}
	out := personaCatalog()
	catalog, err := s.squadCatalog(r.Context())
	if err != nil {
		squadFailure(w, err, true)
		return
	}
	for _, d := range catalog.Definitions {
		if d.Builtin {
			continue
		}
		teams := make([]string, 0, len(d.Teams))
		for _, ref := range d.Teams {
			if strings.HasPrefix(ref.ID, "clawdline.team.") {
				teams = append(teams, strings.TrimPrefix(ref.ID, "clawdline.team."))
			}
		}
		if len(teams) == 0 {
			teams = []string{"custom"}
		}
		out.Personas = append(out.Personas, contract.Persona{
			ID: d.DefinitionID, Teams: teams,
			Name:           contract.PersonaNames{En: d.Name.En, ZhHant: d.Name.ZhHant},
			Summary:        contract.PersonaNames{En: d.Summary.En, ZhHant: d.Summary.ZhHant},
			SuggestedKinds: []string{}, Icon: contract.Icon{Accent: d.Icon.Accent, Cells: d.Icon.Cells}, Source: d.Source,
		})
	}
	if view == "display-v1" {
		writeJSON(w, personaDisplay(out, version))
		return
	}
	writeJSON(w, out)
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

// The display projection keeps the short summary used by tooltips and screen
// readers. Provenance and full role bodies remain on the existing detail reads.
func personaDisplay(catalog contract.PersonaCatalog, held string) contract.PersonaDisplayReply {
	catalog.Personas = append([]contract.Persona{}, catalog.Personas...)
	catalog.License = ""
	for i := range catalog.Personas {
		catalog.Personas[i].Source = ""
	}
	raw, _ := json.Marshal(catalog)
	var canonical any
	_ = json.Unmarshal(raw, &canonical)
	raw, _ = json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	version := hex.EncodeToString(sum[:])
	reply := contract.PersonaDisplayReply{View: "display-v1", Version: version}
	if held == version {
		reply.Unchanged = true
	} else {
		reply.Catalog = &catalog
	}
	return reply
}

func personaCatalogVersion(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
