package projectassistantacp

import (
	"context"
	"encoding/json"
	"strings"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func (s *ownedMCPServer) playbookTool(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if name == "read_charter" {
		tools, ok := s.tools.(app.CharterReadTools)
		if !ok {
			return nil, refusal(pa.ScopeDenied, "charter_project")
		}
		if err := mcpDecode(mcpEmpty(args), &struct{}{}); err != nil {
			return nil, refusal(pa.Invalid, "charter_read")
		}
		return tools.ReadCharter(ctx)
	}
	tools, ok := s.tools.(app.PlaybookTools)
	if !ok {
		return nil, refusal(pa.ScopeDenied, "charter_project")
	}
	switch name {
	case "read_playbooks":
		var q struct {
			ID string `json:"id"`
		}
		if err := mcpDecode(mcpEmpty(args), &q); err != nil {
			return nil, refusal(pa.Invalid, "playbook_read")
		}
		return tools.ReadPlaybooks(ctx, q.ID)
	case "propose_playbook_edit":
		var q struct {
			app.PlaybookEdit
			Text *string `json:"text"`
		}
		if err := mcpDecode(args, &q); err != nil || q.Text == nil || strings.TrimSpace(q.Reason) == "" {
			return nil, refusal(pa.Invalid, "playbook_edit")
		}
		q.PlaybookEdit.Text = *q.Text
		p, err := tools.ProposePlaybookEdit(ctx, q.PlaybookEdit)
		return map[string]any{"id": p.ID, "base_version": p.BaseVersion, "state": "waiting_for_person"}, err
	default:
		return nil, refusal(pa.Unsupported, "playbook_tool")
	}
}

func mcpPlaybookTools() []any {
	return []any{
		map[string]any{
			"name":        "read_charter",
			"description": "Read your Project's current team charter text, workflow and version. Use that version as base_version for a charter or workflow proposal.",
			"inputSchema": map[string]any{"type": "object", "additionalProperties": false},
		},
		map[string]any{
			"name":        "read_playbooks",
			"description": "List existing Playbooks in your Project's team charter with their IDs and the current charter version. Pass an ID to read that Playbook's full text.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"id": map[string]string{"type": "string"}},
			},
		},
		map[string]any{
			"name":        "propose_playbook_edit",
			"description": "Ask the person to approve the proposed title and full Markdown text of an existing Playbook in your Project. Read it with read_playbooks and use that charter version as base_version. Tell the person where to review the proposal on the team charter page. The Playbook changes only after the person applies the proposal.",
			"inputSchema": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{
					"id":           map[string]string{"type": "string"},
					"base_version": map[string]any{"type": "integer", "minimum": 1},
					"title":        map[string]string{"type": "string"},
					"text":         map[string]string{"type": "string"},
					"reason":       map[string]string{"type": "string", "description": "One sentence describing the requested edit for the charter history."},
				},
				"required": []string{"id", "base_version", "title", "text", "reason"},
			},
		},
	}
}

func (s *ownedMCPServer) charterTools(ctx context.Context) []any {
	var list []any
	if charter, ok := s.tools.(app.CharterTools); ok && charter.CharterProposalAvailable(ctx) == nil {
		list = append(list, mcpCharterTool())
		if _, ok := s.tools.(app.CharterWorkflowTools); ok {
			list = append(list, mcpCharterWorkflowTool())
		}
	}
	if playbooks, ok := s.tools.(app.PlaybookTools); ok && playbooks.PlaybooksAvailable(ctx) == nil {
		list = append(list, mcpPlaybookTools()...)
	}
	if conflicts, ok := s.tools.(app.CharterConflictTools); ok && conflicts.CharterConflictAvailable(ctx) == nil {
		list = append(list, mcpCharterConflictTool())
	}
	return list
}
