import { readFileSync } from "node:fs"
import { join } from "node:path"

export type AuditFamily = "unknown_as_certain" | "english_on_zh_screen"
export type AuditDisposition = "open" | "locked"

export interface AuditEvidence {
  file: string
  line: number
  excerpt: string
}

export interface AuditFinding {
  id: string
  family: AuditFamily
  title: string
  cause: string
  disposition: AuditDisposition
  evidence: AuditEvidence[]
}

export interface AuditReport {
  rules: number
  files: number
  findings: AuditFinding[]
  open: AuditFinding[]
  locked: AuditFinding[]
  missing: string[]
  indeterminate: string | null
}

interface Probe {
  file: string
  all: string[]
  any?: string[]
}

interface Rule {
  id: string
  family: AuditFamily
  title: string
  cause: string
  probes: Probe[]
}

const U = "unknown_as_certain" as const
const E = "english_on_zh_screen" as const

// These are source contracts, not line-number bookmarks. Each rule names the
// smallest bad shape that makes one measured issue true. A line may move; the
// issue stays open until the shape changes. Multi-file issues require every
// side of the broken hand-off to remain present.
const RULES: Rule[] = [
  {
    id: "U01",
    family: U,
    title: "An unread session snapshot becomes a zero count",
    cause: "fallback_value",
    probes: [{ file: "web/console/src/App.tsx", all: ["const rows = fleet.snapshot?.sessions ?? []", "totalSessionWords(rows.length)"] }],
  },
  {
    id: "U02",
    family: U,
    title: "A failed Cloud machine list becomes an empty account",
    cause: "rejection_placeholder",
    probes: [{ file: "web/console/src/cloud/CloudGate.tsx", all: ["setMachines([])", "setSyncing(false)"] }],
  },
  {
    id: "U03",
    family: U,
    title: "An unread board becomes zero items and an empty section",
    cause: "fallback_value",
    probes: [{ file: "web/console/src/pages/work/Board.tsx", all: ["board?.counts[section] ?? 0", "rows.length === 0", "workWord(\"sectionEmpty\")"] }],
  },
  {
    id: "U04",
    family: U,
    title: "A relay socket is reported as a healthy machine",
    cause: "wrong_subject",
    probes: [{ file: "web/console/src/cloud/relay-reader.ts", all: ["case \"/v1/health\"", "this.connected()", "ok: true", "served_by: \"cloud-relay\""] }],
  },
  {
    id: "U05",
    family: U,
    title: "The local machine card hard-codes current and paired",
    cause: "literal_certainty",
    probes: [{ file: "web/console/src/legacy/devices-bridge.ts", all: ["freshness: \"current\"", "pairing: \"local\""] }],
  },
  {
    id: "U06",
    family: U,
    title: "Screen failures discard their refusal and retain only a boolean",
    cause: "reason_dropped",
    probes: [{ file: "web/console/src/session/ScreenPanel.tsx", all: ["setFailed(true)", "setFailed(false)"] }],
  },
  {
    id: "U07",
    family: U,
    title: "The refusal guard rejects the daemon's nested refusal envelope",
    cause: "shape_rejected",
    probes: [{ file: "web/core/src/refusal.ts", all: ["typeof (value as Refusal).error === \"string\"", "typeof (value as Refusal).detail === \"string\""] }],
  },
  {
    id: "U08",
    family: U,
    title: "A named daemon refusal reaches Now as unexpected_error",
    cause: "downstream_of_shape_rejected",
    probes: [
      { file: "web/core/src/refusal.ts", all: ["typeof (value as Refusal).error === \"string\""] },
      { file: "web/console/src/pages/now/shared.ts", all: ["return L.failureSentence(e, nowWord(\"unreadable\"))"] },
    ],
  },
  {
    id: "U09",
    family: U,
    title: "A schedule-place refusal becomes an offline boolean",
    cause: "reason_dropped",
    probes: [{ file: "web/console/src/pages/schedules.tsx", all: ["placesFailed = true", ".catch(() =>", "places = null", "T().webOffline"] }],
  },
  {
    id: "U10",
    family: U,
    title: "A failed send bypasses failureSentence and always offers retry",
    cause: "formatter_bypassed",
    probes: [{ file: "web/console/src/session/Transcript.tsx", all: ["T.webFailWithTag", "tag: card.failure", "data-pending-retry"] }],
  },
  {
    id: "U11",
    family: U,
    title: "Missing tmux leaves an initially complete inventory authoritative",
    cause: "reason_recorded_without_state",
    probes: [{ file: "internal/adapters/terminal/tmux.go", all: ["Complete:   true", "exec.LookPath(t.Binary)", "tmux is not installed", "return inv, nil"] }],
  },
  {
    id: "U12",
    family: U,
    title: "Terminal-source reasons stop before the sessions response",
    cause: "cross_layer_reason_dropped",
    probes: [
      { file: "internal/adapters/terminal/tmux.go", all: ["inv.Notes = append(inv.Notes", "tmux list-panes failed:"] },
      { file: "internal/adapters/terminal/iterm_darwin.go", all: ["inv.Notes = append(inv.Notes", "iTerm2 apple event failed:"] },
      { file: "internal/transport/http/sessions.go", all: ["Complete:   inv.Complete,\n\t\t\tProvenance: inv.Provenance", "Sources: scanSources(inv.Sources, inv.Gaps)" ] },
      { file: "internal/contract/zz_generated.go", all: ["type Scan struct {", "Sources []ScanSource" ] },
    ],
  },
  {
    id: "U13",
    family: U,
    title: "Unmeasured Feature attribution becomes zero rows",
    cause: "literal_zero",
    probes: [{ file: "internal/adapters/analytics/worktrees.go", all: ["\"featureRows\": 0", "\"worktrees\":    []any{}"] }],
  },
  {
    id: "U14",
    family: U,
    title: "An unconfigured review classifier claims a complete zero-row read",
    cause: "literal_complete",
    probes: [{ file: "internal/adapters/analytics/query.go", all: ["\"classifier\":           obj{\"configured\": false}", "\"reviewReceipts\": obj{\"status\": \"complete\", \"read\": 0"] }],
  },
  {
    id: "U15",
    family: U,
    title: "An unclassified Cloud retry is labelled as server offline",
    cause: "guessed_code",
    probes: [{ file: "web/console/src/cloud/CloudGate.tsx", all: ["update.error?.code ?? \"offline\"", "at: \"retrying\""] }],
  },
  {
    id: "U16",
    family: U,
    title: "A Cloud access problem loses the machine that produced it",
    cause: "subject_dropped",
    probes: [{ file: "web/console/src/cloud/CloudGate.tsx", all: ["setProblem(code)", "cloudAccessProblem", "{ code: problem }"] }],
  },
  {
    id: "U17",
    family: U,
    title: "The board fallback repeats a refusal code as prose and tag",
    cause: "code_as_sentence",
    probes: [{ file: "web/console/src/pages/work/shared.ts", all: ["failureSentence(e, workWord(\"failed\", { detail: e.code }))"] }],
  },
  {
    id: "E01",
    family: E,
    title: "The drawer writes Dashboard outside the catalog",
    cause: "raw_ui_english",
    probes: [{ file: "web/console/src/App.tsx", all: [">\n            Dashboard\n          </button>"] }],
  },
  {
    id: "E02",
    family: E,
    title: "The schedule summary writes three English labels",
    cause: "raw_ui_english",
    probes: [{ file: "web/console/src/pages/schedules.tsx", all: ["<span>Schedules</span>", "title=\"New schedule\"", "aria-label=\"Scheduled tasks\""] }],
  },
  {
    id: "E03",
    family: E,
    title: "Dashboard panel headings bypass the catalog",
    cause: "raw_ui_english",
    probes: [{ file: "web/console/src/Dashboard.tsx", all: ["Sessions<span", "Obligations", "Tasks<span", "Schedules<span", "Coordinator"] }],
  },
  {
    id: "E04",
    family: E,
    title: "Dashboard renders wire enums and generation notation as prose",
    cause: "wire_value_as_prose",
    probes: [{ file: "web/console/src/Dashboard.tsx", all: ["{row.state} / {row.evidence}", "gen {data.record.generation}"] }],
  },
  {
    id: "E05",
    family: E,
    title: "The reachable Usage page is an English HTML fragment",
    cause: "raw_html_english",
    probes: [{ file: "web/console/src/pages/usage/section.html", all: ["Usage Portfolio", "Recent agent work", "Worth a closer look"] }],
  },
  {
    id: "E06",
    family: E,
    title: "The Projects page retains English static copy",
    cause: "raw_html_english",
    probes: [{ file: "web/console/src/pages/projects/section.html", all: [">Projects</h1>", "Directories an assistant has actually been run in", "Repository lifecycle", "Refresh observation"] }],
  },
  {
    id: "E07",
    family: E,
    title: "A permanently disabled settings control says Loading",
    cause: "raw_ui_english",
    probes: [{ file: "web/console/src/pages/settings.tsx", all: ["id=\"settings-timeline-toggle\"", "Loading…"] }],
  },
  {
    id: "E08",
    family: E,
    title: "Board settings fall back to English before the answer arrives",
    cause: "english_fallback",
    probes: [{ file: "web/console/src/pages/settings/BoardBlock.tsx", all: [": \"Enable Project Board\"", ": \"Loading…\"", ": \"Open projects\"", ": \"AI reading summaries\""] }],
  },
  {
    id: "E09",
    family: E,
    title: "Settings briefly says Oldest first in English",
    cause: "english_fallback",
    probes: [{ file: "web/console/src/pages/settings.tsx", all: [": \"Oldest first\""] }],
  },
  {
    id: "E10",
    family: E,
    title: "The Traditional Chinese work catalog contains Backlog",
    cause: "english_catalog_value",
    probes: [{ file: "web/console/src/pages/work/words.ts", all: ["\"zh-Hant\": {", "tabBacklog: \"Backlog\"", "backlogTitle: \"Backlog\""] }],
  },
  {
    id: "E11",
    family: E,
    title: "The schedule list prints a Go parse error verbatim",
    cause: "producer_prose",
    probes: [{ file: "web/console/src/pages/schedules.tsx", all: ["schedule.error || \"The schedule could not be read.\""] }],
  },
  {
    id: "E12",
    family: E,
    title: "Dashboard prints the same schedule parse error verbatim",
    cause: "producer_prose",
    probes: [{ file: "web/console/src/Dashboard.tsx", all: ["{row.file} — {row.error}"] }],
  },
  {
    id: "E13",
    family: E,
    title: "Invalid schedules use three English fallbacks",
    cause: "raw_ui_english",
    probes: [{ file: "web/console/src/pages/schedules.tsx", all: ["Untitled schedule", "Invalid schedule", ">invalid</span>"] }],
  },
  {
    id: "E14",
    family: E,
    title: "Schedule overlays retain untranslated controls",
    cause: "raw_html_english",
    probes: [{ file: "web/console/src/pages/schedules/overlays.html", all: [">Model</span>", ">Delete</button>", ">Cancel</button>"] }],
  },
  {
    id: "E15",
    family: E,
    title: "The password door prints a refusal message directly",
    cause: "producer_prose",
    probes: [{ file: "web/console/src/door/Door.tsx", all: ["this.say(e.message || T.webDoorWrongPassword)"] }],
  },
  {
    id: "E16",
    family: E,
    title: "Settings chooses refusal detail over failureSentence",
    cause: "producer_prose",
    probes: [{ file: "web/console/src/pages/settings/window/SettingsWindow.tsx", all: ["if (error instanceof RefusalError) return error.detail || error.code"] }],
  },
  {
    id: "E17",
    family: E,
    title: "Cloud settings wraps a daemon message as displayable Error prose",
    cause: "producer_prose",
    probes: [{ file: "web/console/src/pages/settings/cloud.ts", all: ["refusal?.error?.message || `${path} 回答 ${res.status}`"] }],
  },
  {
    id: "E18",
    family: E,
    title: "Schedule push notifications are composed in English by the daemon",
    cause: "daemon_ui_english",
    probes: [
      { file: "internal/app/scheduler.go", all: ["Scheduled run missed its catch-up window.", "Scheduled run could not start: "] },
      { file: "internal/app/schedules.go", all: ["Schedule file ", " is invalid: "] },
    ],
  },
  {
    id: "L01",
    family: U,
    title: "The byte-locked ledger cannot yet explain usage_analytics_busy",
    cause: "locked_copy",
    probes: [{ file: "web/console/src/legacy/js/view/ledger.js", all: ["code === \"graph_not_found\"", "fallback: T.webLedgerFailed"] }],
  },
  {
    id: "L02",
    family: U,
    title: "The byte-locked device view still draws an unknown list as zero",
    cause: "locked_copy",
    probes: [{ file: "web/console/src/legacy/js/view/devices.js", all: ["machine.sessions + \" \" + copy.webSessions"] }],
  },
]

const EXPECTED_RULES = 37
const EXPECTED_OPEN = 24
const EXPECTED_LOCKED = 2

export function dispositionFor(file: string): AuditDisposition {
  const normalized = file.replaceAll("\\", "/")
  return normalized.includes("/legacy/js/") || normalized.includes("/public/strings/") ? "locked" : "open"
}

function lineAt(text: string, offset: number): number {
  return text.slice(0, offset).split("\n").length
}

function oneLine(text: string, offset: number, needle: string): string {
  const start = text.lastIndexOf("\n", offset) + 1
  const end = text.indexOf("\n", offset + needle.length)
  return text.slice(start, end < 0 ? text.length : end).trim().replace(/\s+/g, " ").slice(0, 160)
}

function evidence(root: string, probe: Probe): AuditEvidence | null {
  const text = readFileSync(join(root, probe.file), "utf8")
  const missing = probe.all.find((needle) => !text.includes(needle))
  if (missing) return null
  if (probe.any && !probe.any.some((needle) => text.includes(needle))) return null
  const needle = probe.all[0]!
  const offset = text.indexOf(needle)
  return { file: probe.file, line: lineAt(text, offset), excerpt: oneLine(text, offset, needle) }
}

export function scanAudits(root: string): AuditReport {
  const findings: AuditFinding[] = []
  const missing: string[] = []
  const files = new Set<string>()
  for (const rule of RULES) {
    const found: AuditEvidence[] = []
    try {
      for (const probe of rule.probes) {
        files.add(probe.file)
        const item = evidence(root, probe)
        if (!item) {
          found.length = 0
          break
        }
        found.push(item)
      }
    } catch (error) {
      missing.push(`${rule.id}: ${String(error)}`)
      continue
    }
    if (!found.length) continue
    findings.push({
      id: rule.id,
      family: rule.family,
      title: rule.title,
      cause: rule.cause,
      disposition: found.some((item) => dispositionFor(item.file) === "locked") ? "locked" : "open",
      evidence: found,
    })
  }
  const open = findings.filter((item) => item.disposition === "open")
  const locked = findings.filter((item) => item.disposition === "locked")
  let indeterminate: string | null = null
  if (RULES.length < EXPECTED_RULES) indeterminate = `only ${RULES.length} audit rules; expected at least ${EXPECTED_RULES}`
  else if (files.size < 20) indeterminate = `only ${files.size} source files were inspected`
  else if (missing.length) indeterminate = missing[0]!
  return { rules: RULES.length, files: files.size, findings, open, locked, missing, indeterminate }
}

export function selfCheckAudits(): string | null {
  const caught = sourceRisks("fixture.tsx", `read().catch(() => { setRows([]); setFailed(true) })\n<div>{e.detail}</div>`)
  if (!caught.includes("reason_dropped") || !caught.includes("producer_prose")) {
    return "the audit no longer catches its TypeScript uncertainty and producer-prose fixtures"
  }
  const fixed = sourceRisks("fixture.tsx", `read().catch((e) => setSaid(failureSentence(e, T.failed)))`)
  if (fixed.length) return `the audit rejects its corrected TypeScript fixture: ${fixed.join(", ")}`
  const go = sourceRisks("fixture.go", `return map[string]any{"status": "complete", "read": 0}`)
  if (!go.includes("literal_complete")) return "the audit no longer catches its Go false-completeness fixture"
  return null
}

/** Small, syntax-shaped detectors used by fixtures and by future rules. */
export function sourceRisks(file: string, text: string): string[] {
  const risks = new Set<string>()
  if (/\.catch\s*\(\s*\(\s*\)\s*=>[\s\S]{0,180}(?:set[A-Z]\w*\((?:\[\]|\{\}|true|false|null|0)\)|\w+\s*=\s*(?:\[\]|\{\}|true|false|null|0))/.test(text)) {
    risks.add("reason_dropped")
  }
  if (/\{\s*(?:\w+\.)?(?:message|detail|error)\s*\}/.test(text) || /(?:textContent|innerHTML|set[A-Z]\w*)\s*=*\s*\(?\s*\w+\.(?:message|detail)/.test(text)) {
    risks.add("producer_prose")
  }
  if (file.endsWith(".go") && /["`]status["`]\s*:\s*["`]complete["`][\s\S]{0,80}["`]read["`]\s*:\s*0/.test(text)) {
    risks.add("literal_complete")
  }
  return [...risks]
}

export const AUDIT_EXPECTATIONS = { rules: EXPECTED_RULES, open: EXPECTED_OPEN, locked: EXPECTED_LOCKED }
