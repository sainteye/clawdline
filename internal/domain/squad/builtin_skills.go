package squad

import (
	"embed"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

//go:embed builtin_skills/*.md
var builtinSkillContent embed.FS

type builtinSkillSpec struct {
	Persona, Name, ZhName, Purpose, ZhPurpose, Source, License string
}

// Each source is a reviewed, commit-pinned entry in docs/persona-skill-decisions.json.
// The bundled text is a Clawdline-specific adaptation, not a remote file loaded
// during a session. Its source and content digest are visible in the catalog.
var builtinSkillSpecs = []builtinSkillSpec{
	{"backend", "golang-concurrency", "Go 並行設計", "Design and review Go concurrency", "設計與審查 Go 並行流程", "https://github.com/samber/cc-skills-golang/blob/19a0626ae8565d27a7b7bdf59d8d99d94d7e284c/skills/golang-concurrency/SKILL.md", "MIT"},
	{"frontend", "frontend-component-build", "前端元件實作", "Build accessible reusable components", "建立可重用且可及的前端元件", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/frontend-component-build/SKILL.md", "MIT"},
	{"technical-writer", "documentation-writer", "技術文件寫作", "Write task-focused technical documentation", "撰寫符合讀者任務的技術文件", "https://github.com/github/awesome-copilot/blob/6efe0d035a6415137153bb9b3959589191b7afbe/skills/documentation-writer/SKILL.md", "MIT"},
	{"product-manager", "write-spec", "產品規格撰寫", "Turn evidence into a testable product spec", "把證據整理為可驗收的產品規格", "https://github.com/anthropics/knowledge-work-plugins/blob/da38ec1ee89d41e5380e652a97382695003396e7/product-management/skills/write-spec/SKILL.md", "Apache-2.0"},
	{"ux-researcher", "ux-research", "使用者研究", "Plan and synthesize user research", "規劃並整理使用者研究", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/ux-research/SKILL.md", "MIT"},
	{"accessibility", "accessibility-audit", "無障礙審查", "Audit accessibility with observed evidence", "以可觀察證據檢查無障礙體驗", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/accessibility-audit/SKILL.md", "MIT"},
	{"sre", "monitoring-and-alerting", "監控與告警", "Design measured service signals and alerts", "設計有實測依據的服務訊號與告警", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/monitoring-and-alerting/SKILL.md", "MIT"},
	{"devops", "devops-rollout-plan", "部署計畫", "Plan deployment, verification, and rollback", "規劃部署、驗證與回復", "https://github.com/github/awesome-copilot/blob/6efe0d035a6415137153bb9b3959589191b7afbe/skills/devops-rollout-plan/SKILL.md", "MIT"},
	{"incident-commander", "incident-response", "事故應變", "Coordinate incident triage and recovery", "協調事故分級、緩解與復原", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/incident-response/SKILL.md", "MIT"},
	{"finops", "cost-optimization", "成本優化", "Analyze verified spend and trade-offs", "分析已驗證的支出與取捨", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/cost-optimization/SKILL.md", "MIT"},
	{"ui-designer", "frontend-design", "前端視覺設計", "Develop intentional interface design", "建立有明確方向的介面設計", "https://github.com/anthropics/skills/blob/8a1541c4a3ffa5a20a5a91de0dcf3f0bab1d1ef4/skills/frontend-design/SKILL.md", "Apache-2.0"},
}

func builtinSkills() map[string]Skill {
	out := make(map[string]Skill, len(builtinSkillSpecs))
	for _, spec := range builtinSkillSpecs {
		body, err := builtinSkillContent.ReadFile("builtin_skills/" + spec.Name + ".md")
		if err != nil {
			panic(err) // An embedded file is part of the build, not runtime state.
		}
		p, ok := persona.Known(spec.Persona)
		if !ok {
			panic("unknown built-in skill persona: " + spec.Persona)
		}
		content := strings.TrimSpace(string(body)) + "\n"
		skill := Skill{
			SkillID: "clawdline.skill." + spec.Name,
			Source:  spec.Source, License: spec.License,
			Name:    Names{En: spec.Name, ZhHant: spec.ZhName},
			Purpose: Names{En: spec.Purpose, ZhHant: spec.ZhPurpose},
			Icon:    Icon{Accent: p.Icon.Accent, Cells: p.Icon.Cells},
			Content: content, Builtin: true,
		}
		skill.Digest = Digest(struct {
			Content, Source, License string
		}{content, skill.Source, skill.License})
		skill.Version = "sha256:" + skill.Digest
		out[spec.Persona] = skill
	}
	return out
}
