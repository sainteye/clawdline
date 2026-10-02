package squad

import (
	"embed"
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/persona"
)

//go:embed builtin_skills/*.md
var builtinSkillContent embed.FS

// The previous bundled texts remain addressable by immutable version when a
// Project override or an older Session refers to one after an update.
//
//go:embed builtin_skill_history/*.md
var builtinSkillHistory embed.FS

type builtinSkillSpec struct {
	Persona, Name, ZhName, Purpose, ZhPurpose, Source, License string
}

// Each source is a reviewed, commit-pinned entry in docs/persona-skill-decisions.json,
// or persona.Original for a text written for Clawdline, which carries the
// repository's own licence and is recorded there as an original skill.
// The bundled text is a portable adaptation, not a remote file loaded during
// a session. The current project's instructions still govern its use.
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
	{"code-reviewer", "two-axis-code-review", "雙軸程式碼審查", "Review project rules and requested behavior separately", "分別核對專案規則與需求實作", "https://github.com/mattpocock/skills/blob/c55ee46073ed923f86ce59a5eb3b6d895095d1b7/skills/engineering/code-review/SKILL.md", "MIT"},
	{"content-writer", "humanize-prose", "去除 AI 文字痕跡", "Rewrite machine-sounding prose without adding facts", "改寫 AI 味的文字且不增加事實", "https://github.com/blader/humanizer/blob/225a6f39ac85f76ee48dbad772ea4abe4ed6c9d8/SKILL.md", "MIT"},
	{"minimal-change", "smallest-sufficient-change", "最小充分改動", "Find the smallest change that fully solves the task", "找出完整解決問題的最小改動", "https://github.com/DietrichGebert/ponytail/blob/e3ba2aa6f1e6f0bc4d69eb09c9f0d0a93af56156/skills/ponytail/SKILL.md", "MIT"},
	{"reality-checker", "verification-before-completion", "完成前驗證", "Back every completion claim with fresh evidence", "每個完成宣稱都附上當下的證據", "https://github.com/obra/superpowers/blob/8ca22dba9a94f28898bbce59f2537ff4d87c747d/skills/verification-before-completion/SKILL.md", "MIT"},
	{"evidence-collector", "map-versus-territory", "說法與實況對照", "Check a documented claim against observed behavior", "以實際行為核對文件上的說法", "https://github.com/tjboudreaux/cc-thinking-skills/blob/7b8fece345dfaa11773be7152ccd194589cb5437/skills/thinking-map-territory/SKILL.md", "MIT"},
	{"security", "exploit-path-review", "攻擊路徑審查", "Report only security findings with a complete exploit path", "只回報有完整攻擊路徑的安全問題", "https://github.com/tjboudreaux/cc-thinking-skills/blob/7b8fece345dfaa11773be7152ccd194589cb5437/skills/thinking-red-team/SKILL.md", "MIT"},
	{"test-automation", "tests-that-catch-breaks", "抓得到錯的測試", "Write tests that name and catch a specific break", "寫出能抓到特定錯誤的測試", "https://github.com/obra/superpowers/blob/8ca22dba9a94f28898bbce59f2537ff4d87c747d/skills/test-driven-development/SKILL.md", "MIT"},
	{"support", "bug-report-from-user", "使用者描述轉問題回報", "Turn a user's description into a reproducible bug report", "把使用者的描述整理成可重現的問題回報", "https://github.com/rockscy/solo-skills/blob/80ec4cfc2ff8aee78243538a2aef999e16509bc3/skills/bug-from-user/SKILL.md", "MIT"},
	{"feedback-synthesizer", "research-synthesis", "回饋與研究整理", "Synthesize feedback into decision-ready patterns", "把回饋整理成可支持決策的模式", "https://github.com/rampstackco/claude-skills/blob/3d4510a94a76ead80122c691b5c480f92f3fbe40/skills/discovery-research-synthesis/SKILL.md", "MIT"},
	{"sprint-prioritizer", "opportunity-cost", "機會成本", "Weigh a commitment against its best alternative", "以最佳替代方案衡量每個投入", "https://github.com/tjboudreaux/cc-thinking-skills/blob/7b8fece345dfaa11773be7152ccd194589cb5437/skills/thinking-opportunity-cost/SKILL.md", "MIT"},
	{"performance", "binding-constraint", "找出真正瓶頸", "Measure, then improve the one binding constraint", "先量測，再改善唯一的關鍵瓶頸", "https://github.com/tjboudreaux/cc-thinking-skills/blob/7b8fece345dfaa11773be7152ccd194589cb5437/skills/thinking-theory-of-constraints/SKILL.md", "MIT"},
	{"zero-review-lead", "zero-based-review", "歸零審查", "Re-justify an existing design from zero with role reviewers", "以多角色從零重新檢驗既有設計", persona.Original, "MIT"},
	{"architect", "decision-record", "架構決策紀錄", "Record a technical decision with its alternatives", "記錄技術決策及其替代方案", "https://github.com/affaan-m/ECC/blob/c05b2d6614f62f6db0047669aa4eefb223d478f9/skills/architecture-decision-records/SKILL.md", "MIT"},
}

func builtinSkills() map[string]Skill {
	out := make(map[string]Skill, len(builtinSkillSpecs))
	for _, spec := range builtinSkillSpecs {
		body, err := builtinSkillContent.ReadFile("builtin_skills/" + spec.Name + ".md")
		if err != nil {
			panic(err) // An embedded file is part of the build, not runtime state.
		}
		out[spec.Persona] = builtSkill(spec, body)
	}
	return out
}

func builtSkill(spec builtinSkillSpec, body []byte) Skill {
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
	return skill
}

// HistoricalBuiltinSkill reads a bundled version that is no longer in the
// current catalog. Only the previous released texts have this fallback.
func HistoricalBuiltinSkill(id, version string) (Skill, bool) {
	skill, ok := historicalBuiltinSkillByID(id)
	return skill, ok && skill.Version == version
}

func historicalBuiltinSkillByID(id string) (Skill, bool) {
	for _, spec := range builtinSkillSpecs {
		if id != "clawdline.skill."+spec.Name {
			continue
		}
		body, err := builtinSkillHistory.ReadFile("builtin_skill_history/" + spec.Name + ".md")
		if errors.Is(err, fs.ErrNotExist) {
			return Skill{}, false
		}
		if err != nil {
			panic(err)
		}
		skill := builtSkill(spec, body)
		return skill, true
	}
	return Skill{}, false
}

// webFiles are the extensions a front-end skill is about.
var webFiles = []string{".css", ".html", ".js", ".jsx", ".scss", ".svelte", ".ts", ".tsx", ".vue"}

// builtinSkillWrites are the file extensions a built-in skill is about, for
// the skills whose own "use this skill when" names a kind of code. A skill
// not named here is about a kind of work, not a kind of file, and applies
// to whatever the task writes.
var builtinSkillWrites = map[string][]string{
	"clawdline.skill.golang-concurrency":       {".go"},
	"clawdline.skill.frontend-component-build": webFiles,
	"clawdline.skill.frontend-design":          webFiles,
}

// SkillAppliesToWrites answers whether a skill can apply to a task that
// declared these writes. Unknown is not "no": nil claims, a claim with no
// extension (a directory or a pattern), or a skill with no file signal all
// answer true, so a skill is left out only when every declared write is a
// file of a kind the skill is not about.
func SkillAppliesToWrites(skillID string, claims []string) bool {
	exts, ok := builtinSkillWrites[skillID]
	if !ok || len(claims) == 0 {
		return true
	}
	for _, claim := range claims {
		ext := strings.ToLower(path.Ext(claim))
		if ext == "" || strings.ContainsAny(ext, "*?[") {
			return true
		}
		for _, e := range exts {
			if e == ext {
				return true
			}
		}
	}
	return false
}
