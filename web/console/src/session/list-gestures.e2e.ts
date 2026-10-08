// The three gestures the session list answers on a phone, driven as a finger
// drives them:
//
//   (cd web && npm run build)
//   node --test web/console/src/session/list-gestures.e2e.ts
//
// Real touch events over the DevTools protocol, at 390x844 with touch
// emulation on, against the built console and a stand-in daemon. The harness
// is `address.e2e.ts`'s, with `Input.dispatchTouchEvent` added: a gesture is
// the one thing about this list that no string test can hold, because what
// decides it is which axis the browser gave the page and whether a passive
// listener could answer at all.
//
// **Two of these tests guard behaviour that was here before the swipe was.**
// `pull to refresh` and `the order is held` describe the list as it already
// worked, so they pass with the swipe taken out and fail if either of the two
// things it had to leave alone is broken. They were run that way before the
// swipe existed and the run is in the task's report.
//
// Named `.e2e.ts` rather than `.test.ts` so the unit run over
// `session/*.test.ts` does not start a browser.
import { test, before, after } from "node:test"
import assert from "node:assert/strict"
import { spawn, type ChildProcess } from "node:child_process"
import { createServer, type Server, type ServerResponse } from "node:http"
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, extname, join, normalize, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const dist = resolve(process.env.CLAWDLINE_DIST || resolve(here, "../../dist"))
const shots = process.env.CLAWDLINE_SHOTS || ""
const chrome = process.env.CHROME || "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

// Pane ids of three digits are spelled in pieces: tools/check-private.sh reads
// any `%NNN` in a published file as a pane copied from somebody's machine.
const pane = (n: number) => "%" + n

/** One row per closeability the list can draw, and one more to sort against. */
const SAFE = pane(701)
const BLOCKED = pane(702)
const UNKNOWN = pane(703)
const SPARE = pane(704)
const NEEDS_ATTESTATION = pane(705)
const RETAINED = pane(706)

type ReadingScenario =
  | "normal"
  | "five"
  | "ninety"
  | "expired"
  | "worst"
  | "live-crowded"
  | "status-one"
  | "status-two"
  | "status-three"
  | "acceptance"
  | "machine-pending"
  | "machine-bound"
  | "machine-ranked"
  | "epic"
  | "refresh"
  | "safe-cleared"
  | "unstarted-codex"
  | "unstarted-claude-refresh"
  | "session-gap"
  | "callback-owner"
let readingScenario: ReadingScenario = "normal"
let inlineDecisionFixture = false
let inlineDecisionAnswers = 0
let sessionGap = false
let sessionGapComplete = false

type Row = Record<string, unknown>

/** A Go reading that proves nothing is owed without a retired attestation. */
function safeCloseability(): Row {
  return {
    activity_generation: 3,
    attestation_id: null,
    mover: null,
    obligation_generation: 3,
    observed_at: 1,
    provenance: ["broker", "self"],
    reasons: [],
    session_generation: 1,
    source: { freshness: "current", max_age_seconds: 30, observed_at: 1, provenance: "session_watch" },
    state: "safe",
    version: "cl1_fixture",
  }
}

/** An obligation standing: the agent is still working in this session. */
function blockedCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "session", self: false, session_id: BLOCKED },
    reasons: [{ code: "terminal_working", kind: "obligation", mover: { kind: "session", self: false, session_id: BLOCKED } }],
    state: "blocked",
    version: "cl1_blocked",
  }
}

/** Not known: the reading could not tell which session this terminal is. */
function unknownCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "broker" },
    reasons: [{ code: "session_identity_ambiguous", kind: "evidence", mover: { kind: "broker" } }],
    state: "unknown",
    version: "cl1_unknown",
  }
}

/** The broker's checks passed, but this session has not made its own attestation. */
function needsAttestationCloseability(): Row {
  return {
    ...safeCloseability(),
    attestation_id: null,
    mover: { kind: "session", self: true, session_id: NEEDS_ATTESTATION },
    reasons: [{ code: "not_attested", kind: "attestation", mover: { kind: "session", self: true, session_id: NEEDS_ATTESTATION } }],
    state: "needs_attestation",
    version: "cl1_needs_attestation",
  }
}

function row(id: string, label: string, closeability: Row, movedAt: number, extra: Row = {}): Row {
  return {
    id,
    label,
    backend: "tmux",
    state: "idle",
    work_state: "ready",
    evidence: "process",
    isClaude: true,
    assistant: "claude",
    sessionId: "conversation-" + id,
    cwd: "/tmp/fixture",
    activity: { known: true, at: movedAt },
    closeability,
    ...extra,
  }
}

/**
 * One row for every closeability, and a fifth whose only job is to move.
 *
 * The order is the page's own rule — working first, then the idle rows by when
 * each last moved — so `BLOCKED, SAFE, NEEDS_ATTESTATION, UNKNOWN, SPARE`.
 * Moving `SPARE` to the front of the idle band is what the held order has to
 * refuse to follow.
 */
const MOVED = { safe: 300, needsAttestation: 250, unknown: 200, spare: 100, spareAfter: 400 }
let spareMoved = MOVED.spare

function rows(): Row[] {
  if (readingScenario === "expired") return []
  if (readingScenario === "callback-owner") return [
    row(SAFE, "Heavy callback running", safeCloseability(), 300,
      { heavy_work: { task_id: "callback-heavy", reason: "Go tests" } }),
    row(BLOCKED, "Wait callback running", blockedCloseability(), 200),
    row(UNKNOWN, "Heavy callback queued", unknownCloseability(), 100),
  ]
  if (readingScenario === "unstarted-codex") return [
    row(SAFE, "Codex has not started", safeCloseability(), 300,
      { assistant: "codex", isClaude: false, sessionId: "", identity: "no_record" }),
  ]
  if (readingScenario === "unstarted-claude-refresh") return [
    row(SAFE, "Claude has not started", unknownCloseability(), 300, {
      state: "unknown", sessionId: "", identity: "no_record",
      activity: { known: false, unknown_reason: "no_record" },
      source: { freshness: "unverified", observed_at: Date.now() / 1000 - 90, provenance: "iterm" },
    }),
  ]
  if (readingScenario === "session-gap") {
    const child = row(BLOCKED, "Alpha child", blockedCloseability(), 100, { state: "working" })
    return sessionGap ? [child] : [row(SAFE, "Zulu root", safeCloseability(), 10), child]
  }
  if (readingScenario === "machine-pending" || readingScenario === "machine-bound") {
    return [row(RETAINED, "Machine workspace", safeCloseability(), 500, {
      machine_scope: true,
      cwd: "/fixture/machine-workspace",
      coordinator: readingScenario === "machine-bound" ? { label: "Clawdfather", status: "online", commands: [] } : undefined,
    })]
  }
  if (readingScenario === "machine-ranked") return [
    row(SAFE, "Needs an answer", safeCloseability(), 900, { state: "waiting" }),
    row(RETAINED, "Machine workspace", safeCloseability(), 1, { machine_scope: true }),
  ]
  if (readingScenario === "epic") return [
    row(SAFE, "Epic owner", safeCloseability(), 10, { sessionId: "owner-conversation" }),
    row(BLOCKED, "Feature Root", blockedCloseability(), 100, {
      sessionId: "feature-conversation",
      root_assignment: { id: "root-feature", label: "Feature Root", state: "briefed", ownership: "independent_root" },
      epic_parent: { owner_session_id: "owner-conversation", epic_id: "epic-fixture" },
    }),
    row(UNKNOWN, "Second Feature Root", unknownCloseability(), 90, {
      sessionId: "second-feature-conversation",
      root_assignment: { id: "root-second-feature", label: "Second Feature Root", state: "briefed", ownership: "independent_root" },
      epic_parent: { owner_session_id: "owner-conversation", epic_id: "epic-fixture" },
    }),
  ]
  if (readingScenario === "acceptance") {
    // One Session reported its turn with a Board item deployed and unopened,
    // one with nothing awaiting, and one whose Board reading failed.
    const reported = (acceptance: Row["acceptance"]): Row => ({
      work_state: "milestone_complete",
      disposition: { scope: "session", evidence: "authenticated_session_delivery", title: "Turn summary" },
      acceptance,
    })
    return [
      row(SAFE, "Awaiting", safeCloseability(), 300, reported({
        state: "pending", work_id: "item-a", title: "A deployed Board item with a title long enough to need its ellipsis",
        phase: "deploying", since: 1, count: 2,
      })),
      row(BLOCKED, "Reported", blockedCloseability(), 200, reported({ state: "none" })),
      row(UNKNOWN, "Unreadable", unknownCloseability(), 100, reported({ state: "unknown" })),
      // A daemon from before the field: a report with no answer is not an answer of none.
      row(SPARE, "Older daemon", safeCloseability(), 50, reported(undefined)),
    ]
  }
  if (readingScenario.startsWith("status-")) {
    const extra: Row = {
      work_state: "milestone_complete",
      disposition: {
        scope: "session",
        evidence: "authenticated_session_delivery",
      },
    }
    if (readingScenario === "status-three") {
      extra.owed = {
        note: "還有一個很長的交付決定等待負責人確認",
        person_needed: true,
        since: 1,
      }
    }
    const status = row(RETAINED, "Status density fixture", needsAttestationCloseability(), 500, extra)
    if (readingScenario === "status-one") delete status.closeability
    return [status]
  }
  if (readingScenario === "worst") {
    return [
      row(RETAINED, "A very long Clawdfather session title that must stay inside its card", {
        ...blockedCloseability(),
        reasons: [
          { code: "pending_landing", kind: "obligation", mover: { kind: "broker" } },
          { code: "pending_task", kind: "obligation", mover: { kind: "broker" } },
        ],
      }, 500, {
        tty: "ttys008-with-a-long-terminal-name",
        state: "working",
        work_state: "working",
        line: "Gitifying every package in the repository (1m 53s · downloading dependencies)",
        agents_reading: { state: "complete" },
        agents: [
          { id: "agent-1", at: 1, depth: 1, type: "Explore", what: "one", state: "running" },
          { id: "agent-2", at: 1, depth: 1, type: "Explore", what: "two", state: "running" },
          { id: "agent-3", at: 1, depth: 1, type: "Explore", what: "three", state: "running" },
        ],
        coordinator: { label: "Clawdfather", status: "online", commands: [] },
      }),
    ]
  }
  if (readingScenario === "live-crowded") {
    return [row(RETAINED, "A working session with another live status", blockedCloseability(), 500, {
      state: "working",
      work_state: "working",
      line: "Gitifying every package in the repository (1m 53s · downloading dependencies)",
      shells: [{ id: "shell-1", at: 1, doing: "Building" }],
    })]
  }
  if (readingScenario === "five" || readingScenario === "ninety") {
    const age = readingScenario === "five" ? 5 : 90
    return [
      row(RETAINED, "Earlier terminal reading", safeCloseability(), 500, {
        source: {
          freshness: "unverified",
          observed_at: Date.now() / 1000 - age,
          provenance: "iterm",
        },
      }),
    ]
  }
  const current = [
    row(SAFE, "Alpha is finished", safeCloseability(), MOVED.safe),
    row(BLOCKED, "Bravo is still working", blockedCloseability(), 400, {
      state: "working",
      work_state: "working",
      line: "1m",
    }),
    row(NEEDS_ATTESTATION, "Echo has not checked in", needsAttestationCloseability(), MOVED.needsAttestation),
    row(UNKNOWN, "Charlie cannot be read", unknownCloseability(), MOVED.unknown),
    row(SPARE, "Delta is finished too", safeCloseability(), spareMoved),
  ]
  if (readingScenario !== "refresh") return current
  return current.map((session) => ({
    ...session,
    source: { freshness: "unverified", observed_at: Date.now() / 1000 - 3, provenance: "iterm" },
    closeability: {
      ...session.closeability as Row,
      state: "unknown",
      reasons: [{ code: "session_inventory_stale", kind: "evidence", mover: { kind: "broker" } }],
    },
  }))
}

const ORDER_AT_REST = [BLOCKED, SAFE, NEEDS_ATTESTATION, UNKNOWN, SPARE]
const ORDER_ONCE_SPARE_MOVED = [BLOCKED, SPARE, SAFE, NEEDS_ATTESTATION, UNKNOWN]

// ---- the stand-in daemon

let generation = 0
/** Every `/v1/sessions` read, so a refresh is counted rather than guessed at. */
let listReads = 0
/** Project filters sent by the Board, including a Project-page deep link. */
let workProjectReads: string[] = []
/** `POST /v1/sessions/{id}/close`, with the Idempotency-Key each arrived under. */
let closes: { id: string; key: string; force: unknown; version: unknown }[] = []
let sessionWorkReads = 0
let machineStarts: { path: string; key: string }[] = []
/** Explicit reminder presses received from an assigned Board item detail. */
let workReminders = 0
let delaySessionWork = false
/** Every stream this daemon is holding open, so a new list can be pushed down one. */
const streams = new Set<ServerResponse>()

function snapshot() {
  generation++
  const age = readingScenario === "five" ? 5 : readingScenario === "ninety" ? 90 : 0
  const source = readingScenario === "normal" || readingScenario === "worst" || readingScenario === "live-crowded" || readingScenario === "epic" || readingScenario === "unstarted-codex" || (readingScenario === "session-gap" && (!sessionGap || sessionGapComplete))
    ? { freshness: "current", observed_at: Date.now() / 1000, provenance: "fixture" }
    : readingScenario === "expired"
      ? { freshness: "missing", observed_at: Date.now() / 1000 - 121, provenance: "iterm" }
      : { freshness: "unverified", observed_at: Date.now() / 1000 - age, provenance: "iterm" }
  return {
    at: Date.now(),
    scan: {
      complete: readingScenario !== "refresh" && (!sessionGap || sessionGapComplete),
      completed: { complete: readingScenario !== "refresh" && (!sessionGap || sessionGapComplete), sequence: generation },
      emptyAuthoritative: readingScenario !== "refresh" && (!sessionGap || sessionGapComplete),
      epoch: 1,
      generation,
      provenance: "fixture",
      source,
      notes: readingScenario === "refresh" ? ["session inventory refresh is in progress; rows from a source that has not answered it are unverified"] : [],
      sources: readingScenario === "refresh" ? [
        { source: "iterm", complete: false },
        { source: "ps", complete: false },
        { source: "tmux", complete: false },
      ] : [],
    },
    // The daemon's own answer moves; the page's order is the page's business.
    sessions: rows(),
  }
}

/** A new list down every open stream, as the daemon pushes one when a row moves. */
function pushSessions(): void {
  const frame = "event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n"
  for (const stream of streams) stream.write(frame)
}

const TYPES: Record<string, string> = {
  ".js": "text/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".png": "image/png",
  ".ico": "image/x-icon",
  ".svg": "image/svg+xml",
  ".webmanifest": "application/manifest+json",
}

function json(res: ServerResponse, status: number, body: unknown) {
  res.writeHead(status, { "content-type": "application/json" })
  res.end(JSON.stringify(body))
}

/** The document with its words written in, as `page.go` writes them. */
function document(): string {
  const html = readFileSync(join(dist, "index.html"), "utf8")
  const words = JSON.parse(readFileSync(join(dist, "strings", "zh-Hant.json"), "utf8"))
  words.lang = "zh-Hant"
  words.dir = "ltr"
  const slot = "<script>localStorage.setItem('ui_language','zh-Hant');window.__strings=" + JSON.stringify(words).replaceAll("</", "<\\/") + "</script>"
  return html.replace("<!-- clawdline:strings -->", slot).replace("<!-- clawdline:cloud -->", "")
}

function daemon(): Server {
  return createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://fixture")
    const path = url.pathname
    if (path === "/v1/strings") {
      return json(res, 200, JSON.parse(readFileSync(join(dist, "catalogs", "zh-Hant.json"), "utf8")))
    }
    if (path === "/v1/sessions") {
      listReads++
      return json(res, 200, snapshot())
    }
    if (path === "/v1/health") return json(res, 200, { ok: true })
    if (path === "/v1/machine/usage") return json(res, 200, {
      at: Date.now(), interval_ms: 3000, cores: 4, cpu_percent: 40, load: [1.6, 1.2, 1.0],
      memory_total_bytes: 8 * 1024 ** 3, memory_used_bytes: 4 * 1024 ** 3,
      memory_available_bytes: 4 * 1024 ** 3, swap_total_bytes: 2 * 1024 ** 3,
      swap_used_bytes: 0, groups: [{
        kind: "session", id: SAFE, pid: 10, processes: 3, cpu_percent: 12,
        rss_bytes: 300 * 1024 ** 2, swap_bytes: 0,
      }], others: [],
    })
    if (path === "/v1/places") return json(res, 200, {
      places: [{ id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null }],
      assistants: [{ id: "codex", label: "Codex" }],
    })
    if (path === "/v1/places/%40machine/start/codex" && req.method === "POST") {
      machineStarts.push({ path, key: String(req.headers["idempotency-key"] ?? "") })
      return json(res, 200, { id: pane(707) })
    }
    if (path === "/v1/work/v2/items" && req.method === "GET") {
      workProjectReads.push(url.searchParams.get("project") ?? "")
      return json(res, 200, {
      ok: true,
      rows: [{
        id: "assignment-fixture",
        project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
        kind: "feature",
        title: "Choose an informed owner",
        description: "See the Session before assigning work.",
        phase: "created",
        condition: null,
        area: "unassigned",
        deployment_policy: "agent_decides",
        review_required: false,
        owner_session: null,
        created_at: 1,
        updated_at: 1,
        closed_at: null,
        cycle: 1,
        version: 1,
      }, {
        // Three actions and a Project name that cannot wrap: the header that ran past the
        // card on a phone (2026-09-25).
        id: "header-fixture",
        project: { id: "project-fixture", label: "clawdlinewithalongprojectname", path: "/tmp/fixture", icon: null, available: true },
        kind: "issue",
        title: "Keep the header inside the card",
        description: "Remind, edit and delete fit beside or under the Project name.",
        phase: "implementing",
        condition: null,
        area: "assigned",
        deployment_policy: "agent_decides",
        review_required: false,
        owner_session: SAFE,
        created_at: 1,
        updated_at: 1,
        closed_at: null,
        cycle: 1,
        version: 1,
      }],
      counts: { unassigned: 1, assigned: 1 },
      truncated: false,
      })
    }
    if (path === "/v1/work/v2/proposals" && req.method === "GET") {
      return json(res, 200, { rows: [], truncated: false })
    }
    if (path === "/v1/work/decisions" && req.method === "GET") {
      return json(res, 200, { counts: {}, rows: [], next_cursor: null })
    }
    if (path === "/v1/work/decisions/decision-fixture" && req.method === "POST") {
      inlineDecisionAnswers++
      setTimeout(() => json(res, 200, { ok: true, decision: { state: "answered", answer: "yes" } }), 1000)
      return
    }
    if (path === "/v1/work/v2/items/work-fixture" && req.method === "GET") return json(res, 200, {
      ok: true,
      item: {
        id: "work-fixture",
        project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
        kind: "feature",
        title: "Finish the release receipt",
        description: "Publish the verified receipt after the production check passes.",
        phase: "merging",
        condition: "waiting_user",
        user_action: "Confirm the production release window.",
        area: "merging",
        deployment_policy: "agent_decides",
        review_required: false,
        owner_session: SAFE,
        created_at: 1,
        updated_at: 1,
        closed_at: null,
        cycle: 1,
        version: 2,
        documents: [{
          id: "report-fixture",
          role: "completion_report",
          title: "Completion report",
          body: "## Root cause\\n\\n" + "A verified finding that must remain readable on a phone.\\n\\n".repeat(24),
          reference: "",
          position: 0,
          version: 1,
          created_at: 100,
        }],
      },
    })
    if (path === "/v1/work/v2/items/work-fixture/remind" && req.method === "POST") {
      workReminders++
      return json(res, 200, {
        ok: true,
        item: {
          id: "work-fixture",
          project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
          kind: "feature",
          title: "Finish the release receipt",
          description: "Publish the verified receipt after the production check passes.",
          phase: "merging",
          condition: "waiting_user",
          user_action: "Confirm the production release window.",
          area: "merging",
          deployment_policy: "agent_decides",
          review_required: false,
          owner_session: SAFE,
          created_at: 1,
          updated_at: 1,
          closed_at: null,
          cycle: 1,
          version: 2,
        },
      })
    }
    if (path === "/v1/work/v2/items/done-work-fixture" && req.method === "GET") return json(res, 200, {
      ok: true,
      item: {
        id: "done-work-fixture",
        project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
        kind: "issue",
        title: "Repair the previous release",
        description: "The completed investigation remains readable after the item closes.",
        phase: "done",
        condition: null,
        area: "done",
        deployment_policy: "required",
        review_required: false,
        owner_session: null,
        created_at: 1,
        updated_at: 2,
        closed_at: 2,
        cycle: 1,
        version: 2,
        documents: [{
          id: "older-done-report-fixture",
          role: "completion_report",
          title: "Older completion report",
          body: "## Root cause\n\n" + "A verified finding that must remain readable on a phone.\n\n".repeat(24),
          reference: "",
          position: 0,
          version: 1,
          created_at: 100,
        }, {
          id: "newest-done-report-fixture",
          role: "completion_report",
          title: "Newest completion report",
          body: "## Latest finding\n\nThe latest written conclusion leads the report history.",
          reference: "",
          position: 0,
          version: 1,
          created_at: 200,
        }],
      },
    })
    if (path === "/v1/work/v2/images/todo-image-fixture" && req.method === "GET") {
      res.writeHead(200, { "content-type": "image/png" })
      return res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64"))
    }
    const sessionWork = /^\/v1\/work\/v2\/session-todos\/(.+)$/.exec(path)
    if (sessionWork && req.method === "GET") {
      sessionWorkReads++
      if (readingScenario === "unstarted-codex") {
        res.writeHead(409, { "content-type": "application/json" })
        return res.end(JSON.stringify({ error: "session_unavailable" }))
      }
      const sessionID = decodeURIComponent(sessionWork[1])
      const ownsSafe = sessionID === SAFE || sessionID === "conversation:conversation-" + SAFE
      const assigned = ownsSafe && readingScenario !== "safe-cleared"
        ? [{
            id: "work-fixture",
            project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
            kind: "feature",
            title: "Finish the release receipt",
            description: "Publish the verified receipt after the production check passes.",
            phase: "merging",
            condition: inlineDecisionFixture && inlineDecisionAnswers > 0 ? null : "waiting_user",
            decision_id: inlineDecisionFixture && inlineDecisionAnswers === 0 ? "decision-fixture" : null,
            user_action: "Confirm the production release window.",
            area: "merging",
            deployment_policy: "agent_decides",
            review_required: false,
            owner_session: SAFE,
            created_at: 1,
            updated_at: 1,
            closed_at: null,
            cycle: 1,
            version: 1,
          }]
        : sessionID === BLOCKED
          ? [{
              id: "active-work-fixture",
              project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
              kind: "feature",
              title: "Coordinate the live deployment",
              description: "",
              phase: "implementing",
              condition: null,
              area: "implementing",
              deployment_policy: "required",
              review_required: false,
              owner_session: "conversation-" + BLOCKED,
              created_at: 1,
              updated_at: 1,
              closed_at: null,
              cycle: 1,
              version: 1,
            }]
        : []
      const direct = sessionID === BLOCKED
        ? [{ id: "todo-fixture", text: "Review the release note", created_at: 1, sent_at: 2, read_at: 3, completed_at: null, version: 3,
            images: [{ id: "todo-image-fixture", title: "release-evidence.png", media_type: "image/png", byte_count: 68,
              width: 1, height: 1, position: 0, created_by: "fixture", created_at: 1 }] }]
        : sessionID === NEEDS_ATTESTATION
          ? [{ id: "done-todo-fixture", text: "Verify the hosted console", created_at: 1, sent_at: 2, read_at: 3, completed_at: 4, version: 4 }]
          : []
      const recent = sessionID === BLOCKED || sessionID === NEEDS_ATTESTATION
        ? [{
            id: "done-work-fixture",
            project: { id: "project-fixture", label: "Clawdline", path: "/tmp/fixture", icon: null, available: true },
            kind: "issue",
            title: "Repair the previous release",
            description: "",
            phase: "done",
            condition: null,
            area: "done",
            deployment_policy: "required",
            review_required: false,
            owner_session: null,
            created_at: 1,
            updated_at: 2,
            closed_at: 2,
            cycle: 1,
            version: 2,
            documents: [{
              id: "done-report-fixture",
              role: "completion_report",
              title: "Completion report",
              body: "A durable completion report.",
              reference: "",
              position: 0,
              version: 1,
              created_at: 100,
            }],
          }]
        : []
      const openDecisions = inlineDecisionFixture && ownsSafe && inlineDecisionAnswers === 0
        ? [{ id: "decision-fixture", work_id: "work-fixture", session_id: "conversation-" + SAFE,
          question: "Should the release proceed?", options: [{ id: "yes", label: "Proceed" }, { id: "later", label: "Later" }],
          default: "later", blocking: true, state: "open", created_at: 1, due_at: 9999999999 }]
        : []
      const answer = { ok: true, assigned_items: assigned, recent_items: recent, direct_todos: direct,
        open_decisions: openDecisions, decisions_error: null, truncated: false }
      if (delaySessionWork && !url.searchParams.has("summary")) {
        setTimeout(() => json(res, 200, answer), 900)
        return
      }
      return json(res, 200, answer)
    }
    if (path === "/v1/orchestrator/tasks") {
      const tasks = readingScenario === "session-gap"
        ? [{ id: "task-gap-fixture", state: "briefed", created: 1,
            root: { terminalId: SAFE, sessionId: "conversation-" + SAFE }, child: { terminalId: BLOCKED } }]
        : readingScenario === "callback-owner"
        ? [
            { id: "callback-heavy", kind: "callback", callback_intent: "heavy", state: "briefed", created: 1,
              title: "Go tests", root: { terminalId: SAFE, sessionId: "conversation-" + SAFE } },
            { id: "callback-wait", kind: "callback", callback_intent: "wait", state: "briefed", created: 1,
              title: "Wait for CI", root: { terminalId: BLOCKED, sessionId: "conversation-" + BLOCKED } },
            { id: "callback-queued", kind: "callback", callback_intent: "heavy", state: "briefed", created: 1,
              title: "Go build", root: { terminalId: UNKNOWN, sessionId: "conversation-" + UNKNOWN } },
          ]
        : readingScenario === "worst"
        ? [{
            id: "task-fixture",
            task_id: "task-fixture",
            title: "A child task with a title too long for the phone row",
            state: "briefed",
            created: 1,
            root: { terminalId: pane(700), sessionId: "root-fixture" },
            child: { terminalId: RETAINED },
          }]
        : []
      return json(res, 200, { at: Date.now(), tasks })
    }
    if (path === "/v1/events") {
      res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" })
      res.write("event: sessions\ndata: " + JSON.stringify(snapshot()) + "\n\n")
      streams.add(res)
      req.on("close", () => streams.delete(res))
      return // held open, as the daemon's stream is
    }
    const closing = /^\/v1\/sessions\/(.+)\/close$/.exec(path)
    if (closing && req.method === "POST") {
      let body = ""
      req.on("data", (chunk) => (body += chunk))
      req.on("end", () => {
        let force: unknown = null
        let version: unknown = null
        try {
          const options = JSON.parse(body || "{}") as { force?: unknown; expected_closeability_version?: unknown }
          force = options.force
          version = options.expected_closeability_version
        } catch {
          /* the test reads what arrived, not what it meant */
        }
        closes.push({
          id: decodeURIComponent(closing[1]),
          key: String(req.headers["idempotency-key"] ?? ""),
          force,
          version,
        })
        if (decodeURIComponent(closing[1]) === BLOCKED && force !== true) {
          return json(res, 409, {
            error: "close_blocked",
            detail: "still owed: landing",
            reasons: blockedCloseability().reasons,
          })
        }
        json(res, 200, { ok: true })
      })
      return
    }
    if (path === "/v1/transcript") {
      return json(res, 200, { entries: [], evidence: "process", id: url.searchParams.get("session"), signature: "fixture" })
    }
    if (path.startsWith("/v1/")) return json(res, 404, { error: { code: "not_found", message: path } })
    if (path === "/") {
      res.writeHead(200, { "content-type": "text/html; charset=utf-8" })
      return res.end(document())
    }
    const file = normalize(join(dist, path))
    if (!file.startsWith(dist + "/") || !existsSync(file) || !statSync(file).isFile()) {
      return json(res, 404, { error: { code: "not_found", message: path } })
    }
    res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" })
    res.end(readFileSync(file))
  })
}

// ---- a browser, over the DevTools protocol

type Pending = { resolve: (v: any) => void; reject: (e: Error) => void }

class Browser {
  private seq = 0
  private pending = new Map<number, Pending>()
  private events: { method: string; params: any; sessionId?: string }[] = []
  private waiters: (() => void)[] = []
  private ws: WebSocket

  private constructor(ws: WebSocket) {
    this.ws = ws
    ws.addEventListener("message", (ev) => {
      const msg = JSON.parse(String(ev.data))
      if (msg.id !== undefined) {
        const p = this.pending.get(msg.id)
        this.pending.delete(msg.id)
        if (msg.error) p?.reject(new Error(msg.error.message))
        else p?.resolve(msg.result)
        return
      }
      this.events.push(msg)
      for (const w of this.waiters.splice(0)) w()
    })
  }

  static async connect(url: string): Promise<Browser> {
    const ws = new WebSocket(url)
    await new Promise<void>((ok, fail) => {
      ws.addEventListener("open", () => ok())
      ws.addEventListener("error", () => fail(new Error("could not reach the browser at " + url)))
    })
    return new Browser(ws)
  }

  send(method: string, params: object = {}, sessionId?: string, ms = 15_000): Promise<any> {
    const id = ++this.seq
    this.ws.send(JSON.stringify({ id, method, params, sessionId }))
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id)
        reject(new Error(method + " had no answer within " + ms + "ms"))
      }, ms)
      this.pending.set(id, {
        resolve: (v) => (clearTimeout(timer), resolve(v)),
        reject: (e) => (clearTimeout(timer), reject(e)),
      })
    })
  }

  async event(sessionId: string, method: string, mark: number, ms = 10_000): Promise<void> {
    const deadline = Date.now() + ms
    for (;;) {
      if (this.events.slice(mark).some((e) => e.sessionId === sessionId && e.method === method)) return
      if (Date.now() > deadline) throw new Error("no " + method + " within " + ms + "ms")
      await new Promise<void>((ok) => {
        this.waiters.push(ok)
        setTimeout(ok, 100)
      })
    }
  }

  mark(): number {
    return this.events.length
  }

  close() {
    this.ws.close()
  }
}

/** What the list shows, read in one go so a failure can say all of it. */
const PROBE = `(() => {
  const rows = [...document.querySelectorAll("#rows > li.row")]
  const ptr = document.getElementById("ptr")
  const label = document.getElementById("ptr-label")
  const sheet = document.getElementById("action-confirm")
  const swiped = rows.find((n) => n.dataset.swipe === "open" || n.dataset.swipe === "dragging")
  return {
    order: rows.map((n) => n.dataset.id),
    open: rows.find((n) => n.classList.contains("open"))?.dataset.id ?? null,
    detailName: document.getElementById("detail-name")?.textContent ?? null,
    swiping: swiped ? swiped.dataset.id : null,
    swipeState: swiped ? swiped.dataset.swipe : null,
    swipeX: swiped ? swiped.style.getPropertyValue("--swipe-x") : "",
    action: swiped ? (swiped.querySelector(".swipe-end")?.textContent ?? null) : null,
    actionKind: swiped ? (swiped.querySelector(".swipe-end")?.dataset.closeability ?? null) : null,
    rowState: swiped ? (swiped.querySelector(".state")?.textContent ?? null) : null,
    refreshing: rows.some((n) => n.querySelector(".state")?.getAttribute("data-shape")?.includes("+srcunverified")),
    readingBanner: document.querySelector(".session-reading")?.textContent ?? null,
    retainedText: document.querySelector(".retained-reading")?.textContent ?? null,
    moving: rows.some((n) => getComputedStyle(n).transform !== "none"),
    taskVisible: rows.some((n) => {
      const task = n.querySelector(".task-chip")
      return task && getComputedStyle(task).display !== "none"
    }),
    callbackRows: rows.filter((n) => n.querySelector(".session-callback-active")).map((n) => n.dataset.id),
    ptrHeight: ptr ? ptr.style.height : "",
    ptrWord: label ? label.textContent : "",
    scrollTop: document.getElementById("list-scroll")?.scrollTop ?? -1,
    sheet: sheet && !sheet.hidden ? (document.getElementById("action-confirm-title")?.textContent ?? "") : null,
    sheetSay: sheet && !sheet.hidden ? (document.getElementById("action-confirm-say")?.textContent ?? "") : null,
    completedSummary: sheet && !sheet.hidden ? (document.querySelector(".end-work-completed")?.textContent ?? null) : null,
    completedMarkColor: sheet && !sheet.hidden ? (() => {
      const mark = document.querySelector(".end-work-completed-mark")
      return mark ? getComputedStyle(mark).color : null
    })() : null,
    readyStatusColor: sheet && !sheet.hidden ? (() => {
      const status = document.querySelector(".end-work-status.is-ready")
      return status ? getComputedStyle(status).color : null
    })() : null,
    readyMarkColor: sheet && !sheet.hidden ? (() => {
      const mark = document.querySelector(".end-work-ready-mark")
      return mark ? getComputedStyle(mark).color : null
    })() : null,
    technicalText: sheet && !sheet.hidden ? (document.getElementById("action-confirm-technical")?.textContent ?? null) : null,
    technicalOpen: sheet && !sheet.hidden ? !!document.getElementById("action-confirm-technical")?.hasAttribute("open") : null,
    confirmAction: sheet && !sheet.hidden ? (document.getElementById("action-confirm-go")?.textContent ?? "") : null,
    confirmDisabled: sheet && !sheet.hidden ? !!document.getElementById("action-confirm-go")?.hasAttribute("disabled") : null,
    focused: document.activeElement ? document.activeElement.id : "",
  }
})()`

interface Seen {
  order: string[]
  open: string | null
  detailName: string | null
  swiping: string | null
  swipeState: string | null
  swipeX: string
  action: string | null
  actionKind: string | null
  rowState: string | null
  refreshing: boolean
  readingBanner: string | null
  retainedText: string | null
  moving: boolean
  taskVisible: boolean
  callbackRows: string[]
  ptrHeight: string
  ptrWord: string
  scrollTop: number
  sheet: string | null
  sheetSay: string | null
  completedSummary: string | null
  completedMarkColor: string | null
  readyStatusColor: string | null
  readyMarkColor: string | null
  technicalText: string | null
  technicalOpen: boolean | null
  confirmAction: string | null
  confirmDisabled: boolean | null
  focused: string
}

class Tab {
  private b: Browser
  private session: string
  private target: string
  readonly origin: string

  constructor(b: Browser, session: string, target: string, origin: string) {
    this.b = b
    this.session = session
    this.target = target
    this.origin = origin
  }

  static async open(b: Browser, origin: string): Promise<Tab> {
    const { targetId } = await b.send("Target.createTarget", { url: "about:blank" })
    const { sessionId } = await b.send("Target.attachToTarget", { targetId, flatten: true })
    await b.send("Page.enable", {}, sessionId)
    await b.send("Runtime.enable", {}, sessionId)
    await b.send("Emulation.setDeviceMetricsOverride", { ...PHONE, deviceScaleFactor: 2 }, sessionId)
    // Without this the page is a phone that has no fingers: `ontouchstart` is
    // absent, the listeners are never bound, and every gesture test passes by
    // doing nothing at all.
    await b.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 }, sessionId)
    return new Tab(b, sessionId, targetId, origin)
  }

  close(): Promise<void> {
    return this.b.send("Target.closeTarget", { targetId: this.target }).then(
      () => undefined,
      () => undefined,
    )
  }

  async go(address: string): Promise<void> {
    const mark = this.b.mark()
    await this.b.send("Page.navigate", { url: this.origin + address }, this.session)
    await this.b.event(this.session, "Page.loadEventFired", mark)
  }

  async desktop(): Promise<void> {
    await this.b.send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 900, mobile: false, deviceScaleFactor: 1 }, this.session)
    await this.b.send("Emulation.setTouchEmulationEnabled", { enabled: false }, this.session)
  }

  async phoneWidth(width: number): Promise<void> {
    await this.b.send("Emulation.setDeviceMetricsOverride", { width, height: PHONE.height, mobile: true, deviceScaleFactor: 2 }, this.session)
    await this.b.send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 1 }, this.session)
  }

  async run(expression: string): Promise<any> {
    const { result, exceptionDetails } = await this.b.send(
      "Runtime.evaluate",
      { expression, returnByValue: true, awaitPromise: true },
      this.session,
    )
    if (exceptionDetails) throw new Error(exceptionDetails.exception?.description ?? exceptionDetails.text)
    return result.value
  }

  seen(): Promise<Seen> {
    return this.run(PROBE)
  }

  async until(what: string, ok: (s: Seen) => boolean, ms = 5_000): Promise<Seen> {
    const deadline = Date.now() + ms
    let last: Seen | null = null
    for (;;) {
      try {
        last = await this.seen()
        if (ok(last)) return last
      } catch {
        /* between documents */
      }
      if (Date.now() > deadline) assert.fail(what + "; the page showed " + JSON.stringify(last))
      await new Promise((r) => setTimeout(r, 50))
    }
  }

  /** Where a row is on screen, so a finger can be put on it. */
  centreOf(id: string): Promise<{ x: number; y: number }> {
    return this.run(`(() => {
      const row = [...document.querySelectorAll("#rows > li.row")].find((n) => n.dataset.id === ${JSON.stringify(id)})
      if (!row) throw new Error("no row " + ${JSON.stringify(id)})
      const box = row.getBoundingClientRect()
      return { x: Math.round(box.left + box.width / 2), y: Math.round(box.top + box.height / 2) }
    })()`)
  }

  touchAt = (x: number, y: number) => this.touch("touchStart", [{ x, y }])

  private touch(type: string, points: { x: number; y: number }[]): Promise<void> {
    return this.b.send(
      "Input.dispatchTouchEvent",
      { type, touchPoints: points.map((p) => ({ ...p, radiusX: 8, radiusY: 8, force: 1 })) },
      this.session,
    )
  }

  /**
   * One finger, from `from` to `from + by`, in `steps` moves. The pauses are
   * what makes it a drag rather than a teleport: a page that decides an axis
   * on the first move sees the first move.
   */
  async drag(from: { x: number; y: number }, by: { x: number; y: number }, opts: { steps?: number; hold?: number; release?: boolean } = {}) {
    const steps = opts.steps ?? 8
    await this.touch("touchStart", [from])
    for (let i = 1; i <= steps; i++) {
      await this.touch("touchMove", [{ x: Math.round(from.x + (by.x * i) / steps), y: Math.round(from.y + (by.y * i) / steps) }])
      await new Promise((r) => setTimeout(r, 12))
    }
    if (opts.hold) await new Promise((r) => setTimeout(r, opts.hold))
    if (opts.release !== false) await this.touch("touchEnd", [])
  }

  /** The finger leaves, wherever it was. */
  lift(): Promise<void> {
    return this.touch("touchEnd", [])
  }

  /** A press, as a finger makes it: down and up in the same place. */
  async tap(x: number, y: number) {
    await this.touch("touchStart", [{ x, y }])
    await new Promise((r) => setTimeout(r, 30))
    await this.touch("touchEnd", [])
  }

  async press(selector: string): Promise<void> {
    await this.run(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)})
      if (!el) throw new Error("nothing at " + ${JSON.stringify(selector)})
      el.click()
    })()`)
  }

  async key(key: "ArrowDown" | "Enter" | "Escape" | "Tab", shift = false): Promise<void> {
    const code = key === "ArrowDown" ? 40 : key === "Enter" ? 13 : key === "Tab" ? 9 : 27
    const modifiers = shift ? 8 : 0
    await this.b.send("Input.dispatchKeyEvent", { type: "keyDown", key, code: key, windowsVirtualKeyCode: code, modifiers }, this.session)
    await this.b.send("Input.dispatchKeyEvent", { type: "keyUp", key, code: key, windowsVirtualKeyCode: code, modifiers }, this.session)
  }

  async accessibilityOptions(): Promise<string[]> {
    const tree = await this.b.send("Accessibility.getFullAXTree", {}, this.session)
    return tree.nodes.filter((node: any) => node.role?.value === "option").map((node: any) => node.name?.value ?? "")
  }

  async mouseAt(x: number, y: number, click = false): Promise<void> {
    await this.b.send("Input.dispatchMouseEvent", { type: "mouseMoved", x, y }, this.session)
    if (!click) return
    await this.b.send("Input.dispatchMouseEvent", { type: "mousePressed", x, y, button: "left", clickCount: 1 }, this.session)
    await this.b.send("Input.dispatchMouseEvent", { type: "mouseReleased", x, y, button: "left", clickCount: 1 }, this.session)
  }

  /** A picture of the phone, for the report. */
  async shot(name: string): Promise<void> {
    if (!shots) return
    const { data } = await this.b.send("Page.captureScreenshot", { format: "png" }, this.session)
    writeFileSync(join(shots, name + ".png"), Buffer.from(data, "base64"))
  }
}

// ---- the run

async function inTab(body: (tab: Tab) => Promise<void>) {
  const tab = await Tab.open(browser, origin)
  try {
    await body(tab)
  } finally {
    await tab.close()
  }
}

let server: Server
let browserProcess: ChildProcess
let browser: Browser
let profile: string
let origin: string
const PHONE = { width: 390, height: 844, mobile: true }

before(async () => {
  assert.ok(existsSync(join(dist, "index.html")), "no built console at " + dist + "; run `npm run build` in web first")
  assert.ok(existsSync(chrome), "no Chrome at " + chrome + "; set CHROME")
  server = daemon()
  await new Promise<void>((ok) => server.listen(0, "127.0.0.1", ok))
  const address = server.address()
  origin = "http://127.0.0.1:" + (typeof address === "object" && address ? address.port : 0)
  profile = mkdtempSync(join(tmpdir(), "clawdline-gestures-"))
  browserProcess = spawn(chrome, [
    "--headless=new",
    "--remote-debugging-port=0",
    "--user-data-dir=" + profile,
    "--no-first-run",
    "--no-default-browser-check",
    "about:blank",
  ])
  const endpoint = await new Promise<string>((ok, fail) => {
    let said = ""
    const timer = setTimeout(() => fail(new Error("Chrome did not start: " + said)), 20_000)
    browserProcess.stderr?.on("data", (chunk) => {
      said += String(chunk)
      const found = /DevTools listening on (ws:\/\/\S+)/.exec(said)
      if (found) {
        clearTimeout(timer)
        ok(found[1])
      }
    })
  })
  browser = await Browser.connect(endpoint)
})

after(async () => {
  browser?.close()
  if (browserProcess && browserProcess.exitCode === null) {
    const gone = new Promise((ok) => browserProcess.once("exit", ok))
    browserProcess.kill()
    await gone
  }
  server?.closeAllConnections()
  await new Promise<void>((ok) => (server ? server.close(() => ok()) : ok()))
  if (profile) rmSync(profile, { recursive: true, force: true, maxRetries: 5 })
})

/** The list, arrived and at rest, before a finger touches it. */
async function list(tab: Tab): Promise<Seen> {
  spareMoved = MOVED.spare
  closes = []
  await tab.go("/")
  return tab.until("the list arrives in its resting order", (s) => s.order.join() === ORDER_AT_REST.join())
}

test("an incomplete session reading keeps a confirmed root and its child together", () =>
  inTab(async (tab) => {
    readingScenario = "session-gap"
    sessionGap = false
    sessionGapComplete = false
    try {
      await tab.go("/")
      await tab.until("the confirmed parent and child arrive", (s) =>
        s.order.join() === [SAFE, BLOCKED].join() && s.taskVisible)
      sessionGap = true
      pushSessions()
      await new Promise((resolve) => setTimeout(resolve, 200))
      const after = await tab.seen()
      assert.deepEqual(after.order, [SAFE, BLOCKED])
      sessionGapComplete = true
      pushSessions()
      await tab.until("a complete reading confirms the root is gone", (s) => s.order.join() === BLOCKED)
    } finally {
      sessionGap = false
      sessionGapComplete = false
      readingScenario = "normal"
    }
  }))

test("a phone marks only the Session whose heavy callback is running", () =>
  inTab(async (tab) => {
    readingScenario = "callback-owner"
    try {
      await tab.go("/")
      await tab.until("the callback owner is marked", (seen) => seen.callbackRows.join() === SAFE)
      const badge = await tab.run(`(() => {
        const row = document.querySelector("#rows > li.row[data-id='${SAFE}']")
        const mark = row?.querySelector(".session-callback-active")
        return { text: mark?.textContent, label: mark?.getAttribute("aria-label"), title: mark?.getAttribute("title"),
          visible: !!mark && mark.getBoundingClientRect().width > 0,
          clipped: !!mark && mark.scrollWidth > mark.clientWidth + 1,
          listWidth: document.querySelector("#list-scroll")?.scrollWidth,
          viewportWidth: window.innerWidth }
      })()`)
      assert.equal(badge.text, "🏗️")
      assert.equal(badge.label, "重工作業進行中 · Go tests")
      assert.ok(badge.title?.includes("Go tests"), JSON.stringify(badge))
      assert.equal(badge.visible, true)
      assert.equal(badge.clipped, false)
      assert.ok(badge.listWidth <= badge.viewportWidth, JSON.stringify(badge))
    } finally {
      readingScenario = "normal"
    }
  }))

test("keyboard moves through focused options and Enter opens the highlighted Session on both layouts", () =>
  inTab(async (tab) => {
    for (const size of ["phone", "desktop"] as const) {
      if (size === "desktop") await tab.desktop()
      await list(tab)
      await tab.run(`document.querySelector('#rows > li.row').focus()`)
      for (const id of [SAFE, NEEDS_ATTESTATION]) {
        await tab.key("ArrowDown")
        const state = await tab.run(`(() => ({
          selected: document.querySelector('#rows > li.row[aria-selected="true"]')?.dataset.id,
          focused: document.activeElement?.dataset.id
        }))()`)
        assert.deepEqual(state, { selected: id, focused: id })
      }
      await tab.key("Enter")
      assert.equal(await tab.run(`document.querySelector('#rows > li.row.open')?.dataset.id`), NEEDS_ATTESTATION)
    }
  }))

test("the phone option exposes the legacy identity and state without a crown button", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until("the legacy row arrives", (s) => s.order.join() === RETAINED)
      const names = await tab.accessibilityOptions()
      const name = names.find((value) => value.includes("A very long Clawdfather session title")) ?? ""
      assert.match(name, /Clawdfather/)
      assert.doesNotMatch(name, /項未了結/, "the list does not read closeability aloud")
      assert.equal(await tab.run(`document.querySelector('#rows > li.row .coordinator-mark')?.getAttribute('aria-hidden')`), "true")
      assert.equal(await tab.run(`document.querySelector('#rows > li.row .coordinator-mark')?.closest('button, [role=button]') !== null`), false)
    } finally {
      readingScenario = "normal"
    }
  }))

test("the rebuilt legacy crown does not hover as a control and opens only its Session row", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.desktop()
      await tab.go("/")
      await tab.until("the legacy row arrives", (s) => s.order.join() === RETAINED)
      const before = await tab.run(`(() => {
        const mark = document.querySelector('#rows > li.row .coordinator-mark')
        const box = mark.getBoundingClientRect()
        const style = getComputedStyle(mark)
        return { x: box.left + box.width / 2, y: box.top + box.height / 2,
          pointer: style.pointerEvents, background: style.backgroundColor, border: style.borderColor,
          target: getComputedStyle(mark, '::before').display }
      })()`)
      assert.equal(before.pointer, "none")
      assert.equal(before.target, "none")
      await tab.mouseAt(before.x, before.y)
      const hovered = await tab.run(`(() => {
        const style = getComputedStyle(document.querySelector('#rows > li.row .coordinator-mark'))
        return { background: style.backgroundColor, border: style.borderColor }
      })()`)
      assert.deepEqual(hovered, { background: before.background, border: before.border })
      await tab.mouseAt(before.x, before.y, true)
      assert.equal(await tab.run(`document.querySelector('#rows > li.row.open')?.dataset.id`), RETAINED)
      assert.equal(await tab.run(`document.querySelector('.coordinator-controls-sheet:not([hidden])') !== null`), false)
    } finally {
      readingScenario = "normal"
    }
  }))

test("closing the new Session sheet returns focus to its opener", () =>
  inTab(async (tab) => {
    await list(tab)
    for (const mode of ["Escape", "button", "backdrop"] as const) {
      await tab.run(`document.getElementById('start-go').focus()`)
      await tab.press("#start-go")
      assert.equal(await tab.run(`document.activeElement?.id`), "start-title")
      if (mode === "Escape") await tab.key("Escape")
      if (mode === "button") await tab.press("#start-close")
      if (mode === "backdrop") await tab.press("#start")
      assert.deepEqual(await tab.run(`({ hidden: document.getElementById('start').hidden, focused: document.activeElement?.id })`),
        { hidden: true, focused: "start-go" })
    }
  }))

test("the machine dashboard owns Clawdfather creation on phone and desktop", () =>
  inTab(async (tab) => {
    for (const layout of ["phone", "narrow", "desktop"] as const) {
      if (layout === "narrow") await tab.phoneWidth(320)
      if (layout === "desktop") await tab.desktop()
      await list(tab)
      await tab.press("#start-go")
      assert.equal(await tab.run(`document.querySelector('#start-resume .machine-start') === null`), true)
      await tab.press("#start-close")
      await tab.press("#counts")
      await tab.run(`new Promise((resolve, reject) => {
        const started = Date.now()
        const check = () => {
          if (document.querySelector('.machine-gauges')) resolve(true)
          else if (Date.now() - started > 5000) reject(new Error('machine usage did not load'))
          else setTimeout(check, 20)
        }
        check()
      })`)
      const shown = await tab.run(`(() => {
        const machine = document.querySelector('.machine-clawdfather')
        const rect = machine?.getBoundingClientRect()
        const crown = machine?.querySelector('.clawdfather-crown')
        return { name: machine?.textContent, crown: crown ? getComputedStyle(crown).backgroundColor : '',
          crownShape: crown ? getComputedStyle(crown).clipPath : '',
          width: rect?.width, right: rect?.right, viewport: window.innerWidth }
      })()`)
      assert.match(shown.name, /^(開啟|Open) Clawdfather$/)
      assert.equal(shown.crown, "rgb(240, 199, 94)")
      assert.notEqual(shown.crownShape, "none")
      assert.ok(shown.width <= shown.viewport)
      assert.ok(shown.right <= shown.viewport)
      await tab.shot("machine-dashboard-" + layout)
      await tab.press(".machine-clawdfather")
      assert.equal(await tab.run(`document.getElementById('machine-title') === null`), true)
      assert.equal(await tab.run(`document.getElementById('start-title')?.textContent`), "Clawdfather")
      await tab.run(`new Promise((resolve, reject) => {
        const began = Date.now()
        const check = () => {
          const button = document.getElementById('start-machine-action')
          if (button instanceof HTMLButtonElement && !button.disabled) return resolve(true)
          if (Date.now() - began > 5000) return reject(new Error('machine action did not become available'))
          setTimeout(check, 50)
        }
        check()
      })`)
      assert.equal(await tab.run(`document.querySelector('#start-list .place') === null`), true)
      await tab.shot("machine-start-" + layout)
      await tab.press("#start-close")
    }
  }))

test("the focused machine start sheet keeps Tab inside and gives focus back", () =>
  inTab(async (tab) => {
    for (const layout of ["phone", "desktop"] as const) {
      if (layout === "desktop") await tab.desktop()
      await list(tab)
      await tab.press("#counts")
      await tab.press(".machine-clawdfather")
      await tab.run(`new Promise((resolve, reject) => {
        const started = Date.now()
        const check = () => {
          const row = document.querySelector('#start-machine-action')
          if (row instanceof HTMLButtonElement && !row.disabled) return resolve(true)
          if (Date.now() - started > 5000) return reject(new Error('machine start did not become available'))
          setTimeout(check, 50)
        }
        check()
      })`)
      await tab.key("Tab")
      assert.equal(await tab.run(`document.activeElement?.closest('#start-sheet') !== null`), true)
      await tab.run(`document.getElementById('start-close').focus()`)
      await tab.key("Tab")
      const wrapped = await tab.run(`({ inside: document.activeElement?.closest('#start-sheet') !== null, focused: document.activeElement?.id })`)
      assert.equal(wrapped.inside && wrapped.focused !== "start-close", true, JSON.stringify(wrapped))
      await tab.key("Tab", true)
      assert.equal(await tab.run(`document.activeElement?.id`), "start-close")
      await tab.key("Escape")
      assert.deepEqual(await tab.run(`({ hidden: document.getElementById('start').hidden, focused: document.activeElement?.id })`),
        { hidden: true, focused: "counts" })
      for (const mode of ["button", "backdrop"] as const) {
        await tab.press("#counts")
        await tab.press(".machine-clawdfather")
        if (mode === "button") await tab.press("#start-close")
        else await tab.press("#start")
        assert.deepEqual(await tab.run(`({ hidden: document.getElementById('start').hidden, focused: document.activeElement?.id })`),
          { hidden: true, focused: "counts" })
      }
    }
  }))

test("the dashboard creation button uses the guarded machine start route", () =>
  inTab(async (tab) => {
    machineStarts = []
    await list(tab)
    await tab.press("#counts")
    await tab.press(".machine-clawdfather")
    await tab.run(`new Promise((resolve, reject) => {
      const started = Date.now()
      const check = () => {
        const button = document.getElementById('start-machine-action')
        if (button instanceof HTMLButtonElement && !button.disabled) resolve(true)
        else if (Date.now() - started > 5000) reject(new Error('machine start did not become available'))
        else setTimeout(check, 50)
      }
      check()
    })`)
    await tab.press("#start-machine-action")
    for (let i = 0; i < 40 && machineStarts.length === 0; i++) await new Promise((resolve) => setTimeout(resolve, 50))
    assert.equal(machineStarts.length, 1)
    assert.equal(machineStarts[0].path, "/v1/places/%40machine/start/codex")
    assert.match(machineStarts[0].key, /.+/)
  }))

test("machine registration remains visible after a fresh Session read and clears on receipt", () =>
  inTab(async (tab) => {
    try {
      readingScenario = "machine-pending"
      await tab.go("/")
      await tab.until("the machine Session arrives", (s) => s.order.join() === RETAINED)
      const pending = await tab.run(`document.querySelector('#rows .machine-registration')?.textContent`)
      assert.match(pending, /clawdline coordinator bind/)
      const pendingIdentity = await tab.run(`(() => {
        const row = document.querySelector('#rows > li.row')
        const canvas = row?.querySelector('canvas.mark')
        const pixel = canvas?.getContext('2d')?.getImageData((canvas?.width || 0) / 8 + 1, 0, 1, 1).data
        return { name: row?.querySelector('.label')?.textContent, crown: !!row?.querySelector('.clawdfather-crown'),
          width: canvas?.width, height: canvas?.height, pixel: pixel ? [...pixel] : null }
      })()`)
      assert.equal(pendingIdentity.name, "Clawdfather")
      assert.equal(pendingIdentity.crown, false)
      assert.equal(pendingIdentity.width, pendingIdentity.height * 2)
      assert.deepEqual(pendingIdentity.pixel, [217, 119, 87, 255])
      await tab.run(`document.querySelector('#rows > li.row')?.click()`)
      assert.equal(await tab.run(`document.getElementById('detail-name')?.textContent`), "Clawdfather")
      assert.match(await tab.run(`document.getElementById('detail-sub')?.textContent`), /Registration pending|待登記/)
      assert.equal(await tab.run(`document.getElementById('detail-mark')?.width`), pendingIdentity.width * 5 / 4)
      readingScenario = "machine-bound"
      await tab.go("/")
      await tab.until("the bound machine Session arrives", (s) => s.order.join() === RETAINED)
      assert.equal(await tab.run(`document.querySelector('#rows .machine-registration') === null`), true)
      assert.equal(await tab.run(`document.querySelector('#rows .coordinator-identity') !== null`), true)
      assert.equal(await tab.run(`document.querySelector('#rows .label')?.textContent`), "Clawdfather")
      assert.equal(await tab.run(`document.querySelector('#rows .clawdfather-crown') !== null`), true)
    } finally {
      readingScenario = "normal"
    }
  }))

test("machine dashboard opens the existing Clawdfather and its icon offers draft suggestions", () =>
  inTab(async (tab) => {
    try {
      readingScenario = "machine-bound"
      await tab.go("/")
      await tab.until("the machine Session arrives", (s) => s.order.join() === RETAINED)
      await tab.press("#counts")
      assert.match(await tab.run(`document.querySelector('.machine-clawdfather')?.textContent`), /前往 Clawdfather/)
      await tab.press(".machine-clawdfather")
      await tab.until("the existing Clawdfather opens", (s) => s.detailName === "Clawdfather")
      assert.equal(await tab.run(`document.getElementById('start')?.hidden`), true)
      assert.match(await tab.run(`document.getElementById('detail-snippets')?.getAttribute('aria-label')`), /Clawdfather/)
      await tab.press("#detail-snippets")
      await tab.shot("clawdfather-suggestions-phone")
      assert.equal(await tab.run(`document.querySelector('#snippets:not([hidden])') === null`), true)
      assert.equal(await tab.run(`document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button').length`), 5)
      const icons = await tab.run(`(() => [...document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button')].map((button) => ({
        title: button.textContent.trim(),
        svg: button.querySelector('svg')?.outerHTML || '',
        shape: [...button.querySelectorAll('svg path, svg rect, svg circle')].map((part) => part.outerHTML).join(''),
      })))()`)
      assert.deepEqual(icons.map((icon: { title: string }) => icon.title), [
        "整理 Session 進度", "檢查機器狀況", "檢視 Clawdline 設定", "匯入或匯出專案設定", "建立看板項目並派工",
      ])
      assert.equal(new Set(icons.map((icon: { shape: string }) => icon.shape)).size, 5)
      for (const icon of icons) {
        assert.match(icon.svg, /class="sidebar-icon"/)
        assert.match(icon.svg, /aria-hidden="true"/)
        assert.match(icon.svg, /focusable="false"/)
        assert.ok(icon.shape)
      }
      const geometry = () => tab.run(`(() => {
        const sheet = document.querySelector('#clawdfather-suggestions .sheet')
        const list = sheet.querySelector('.clawdfather-suggestions-list')
        return { viewport: innerWidth, sheetRight: sheet.getBoundingClientRect().right,
          sheetOverflow: sheet.scrollWidth - sheet.clientWidth, listOverflow: list.scrollWidth - list.clientWidth,
          rows: [...list.querySelectorAll('button')].map((button) => {
            const box = button.getBoundingClientRect()
            const icon = button.querySelector('svg').getBoundingClientRect()
            const label = button.querySelector('span').getBoundingClientRect()
            return { left: box.left, right: box.right, height: box.height, overflow: button.scrollWidth - button.clientWidth,
              iconLeft: icon.left, iconRight: icon.right, iconMiddle: (icon.top + icon.bottom) / 2,
              labelLeft: label.left, labelMiddle: (label.top + label.bottom) / 2,
              color: getComputedStyle(button).color, iconColor: getComputedStyle(button.querySelector('svg')).color }
          }) }
      })()`)
      for (const width of [390, 320]) {
        await tab.phoneWidth(width)
        const layout = await geometry()
        assert.equal(layout.viewport, width)
        assert.ok(layout.sheetRight <= width + 1)
        assert.ok(layout.sheetOverflow <= 1 && layout.listOverflow <= 1)
        for (const row of layout.rows) {
          assert.ok(row.height >= 48, `suggestion row is ${row.height}px high`)
          assert.ok(row.overflow <= 1 && row.left >= -1 && row.right <= width + 1)
          assert.ok(row.iconLeft >= row.left && row.iconRight < row.labelLeft)
          assert.ok(Math.abs(row.iconMiddle - row.labelMiddle) <= 1)
          assert.equal(row.iconColor, row.color)
        }
        await tab.shot(`clawdfather-suggestions-${width}-dark`)
      }
      await tab.desktop()
      const desktopLayout = await geometry()
      assert.equal(desktopLayout.viewport, 1280)
      assert.ok(desktopLayout.sheetOverflow <= 1 && desktopLayout.listOverflow <= 1)
      assert.ok(desktopLayout.rows.every((row: { overflow: number; height: number }) => row.overflow <= 1 && row.height >= 48))
      await tab.shot("clawdfather-suggestions-desktop-dark")
      // The Console currently ships a dark palette only. Exercise this sheet against light tokens
      // so the icons' inherited color remains readable if a light palette is introduced.
      await tab.run(`(() => {
        const style = document.documentElement.style
        for (const [name, value] of Object.entries({ '--card': '#ffffff', '--card-hi': '#f3f4f6',
          '--line': '#d7d9df', '--ink': '#1b1d21', '--dim': '#4b5563' })) style.setProperty(name, value)
      })()`)
      const lightLayout = await geometry()
      assert.ok(lightLayout.rows.every((row: { color: string; iconColor: string }) =>
        row.color === "rgb(27, 29, 33)" && row.iconColor === row.color))
      await tab.shot("clawdfather-suggestions-desktop-light-palette")
      await tab.phoneWidth(320)
      const lightPhone = await geometry()
      assert.ok(lightPhone.sheetOverflow <= 1 && lightPhone.listOverflow <= 1)
      assert.ok(lightPhone.rows.every((row: { overflow: number; iconRight: number; labelLeft: number }) =>
        row.overflow <= 1 && row.iconRight < row.labelLeft))
      await tab.shot("clawdfather-suggestions-320-light-palette")
      assert.equal(await tab.run(`document.activeElement?.textContent?.trim()`), "整理 Session 進度")
      await tab.key("Tab")
      assert.equal(await tab.run(`document.activeElement?.textContent?.trim()`), "檢查機器狀況")
      await tab.key("Tab", true)
      assert.equal(await tab.run(`document.activeElement?.textContent?.trim()`), "整理 Session 進度")
      await tab.run(`document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button')[1].focus()`)
      pushSessions()
      await tab.run(`new Promise((resolve) => setTimeout(resolve, 100))`)
      assert.equal(await tab.run(`document.activeElement?.textContent?.trim()`), "檢查機器狀況")
      await tab.press("#clawdfather-suggestions .clawdfather-suggestions-list button:first-child")
      assert.equal(await tab.run(`document.getElementById('clawdfather-suggestions') === null`), true)
      assert.match(await tab.run(`document.getElementById('msg')?.textContent`), /整理這台機器目前各 Session/)
      assert.equal(await tab.run(`document.activeElement?.id`), "msg")
      await tab.press("#detail-snippets")
      await tab.key("Escape")
      assert.equal(await tab.run(`document.activeElement?.id`), "detail-snippets")
      await tab.run(`document.documentElement.lang = 'en'`)
      await tab.press("#detail-snippets")
      assert.deepEqual(await tab.run(`([...document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button')].map((button) => button.textContent.trim()))`), [
        "Summarize Sessions", "Check machine status", "Review Clawdline settings",
        "Import or export project settings", "Create a Board item and delegate",
      ])
      assert.deepEqual(await tab.run(`([...document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button')].map((button) =>
        [...button.querySelectorAll('svg path, svg rect, svg circle')].map((part) => part.outerHTML).join('')))`),
      icons.map((icon: { shape: string }) => icon.shape))
      const edge = (index: number, side: "left" | "right") => tab.run(`(() => {
        const box = document.querySelectorAll('#clawdfather-suggestions .clawdfather-suggestions-list button')[${index}].getBoundingClientRect()
        return { x: Math.round(${JSON.stringify(side)} === 'left' ? box.left + 3 : box.right - 3), y: Math.round(box.top + box.height / 2) }
      })()`)
      const left = await edge(0, "left")
      await tab.tap(left.x, left.y)
      assert.equal(await tab.run(`document.getElementById('clawdfather-suggestions') === null`), true)
      assert.match(await tab.run(`document.getElementById('msg')?.textContent`), /Summarize the progress of Sessions/)
      await tab.press("#detail-snippets")
      const right = await edge(4, "right")
      await tab.tap(right.x, right.y)
      assert.equal(await tab.run(`document.getElementById('clawdfather-suggestions') === null`), true)
      assert.match(await tab.run(`document.getElementById('msg')?.textContent`), /create a Board item/i)
    } finally {
      readingScenario = "normal"
    }
  }))

test("an ordinary Session keeps the Snippets icon", () =>
  inTab(async (tab) => {
    await list(tab)
    await tab.press(`#rows > li.row[data-id="${SAFE}"]`)
    assert.match(await tab.run(`document.getElementById('detail-snippets')?.getAttribute('aria-label')`), /常用句|Snippets/)
    await tab.press("#detail-snippets")
    assert.equal(await tab.run(`document.getElementById('snippets')?.hidden`), false)
    assert.equal(await tab.run(`document.getElementById('clawdfather-suggestions') === null`), true)
  }))

test("Clawdfather stays first above a waiting Session on phone and desktop", () =>
  inTab(async (tab) => {
    try {
      readingScenario = "machine-ranked"
      await tab.go("/")
      await tab.until("Clawdfather leads on phone", (s) => s.order.join() === [RETAINED, SAFE].join())
      const spoken = await tab.accessibilityOptions()
      assert.match(spoken[0] ?? "", /Clawdfather/)
      assert.match(spoken[1] ?? "", /Needs an answer/)
      await tab.phoneWidth(320)
      await tab.go("/")
      await tab.until("Clawdfather leads on a 320px phone", (s) => s.order.join() === [RETAINED, SAFE].join())
      const narrow = await tab.run(`(() => {
        const list = document.getElementById('list-scroll')
        const waiting = document.querySelector('#rows > li.row[data-id="${SAFE}"]')
        const box = waiting?.getBoundingClientRect()
        return { width: innerWidth, listWidth: list?.clientWidth, scrollWidth: list?.scrollWidth,
          waitingState: waiting?.dataset.state, waitingBottom: box?.bottom,
          open: document.querySelector('#rows > li.row.open')?.dataset.id ?? null }
      })()`)
      assert.equal(narrow.width, 320)
      assert.equal(narrow.waitingState, "waiting")
      assert.equal(narrow.open, null)
      assert.ok(narrow.scrollWidth <= narrow.listWidth, JSON.stringify(narrow))
      assert.ok(narrow.waitingBottom <= PHONE.height, JSON.stringify(narrow))
      const beforeRefresh = listReads
      await tab.drag({ x: 160, y: 160 }, { x: 0, y: 220 }, { steps: 10, release: false })
      await tab.lift()
      await tab.until("the narrow phone refresh reads again", () => listReads > beforeRefresh)
      await tab.until("Clawdfather stays first after refresh", (s) => s.order.join() === [RETAINED, SAFE].join())
      const finger = await tab.centreOf(SAFE)
      await tab.drag(finger, { x: -90, y: 0 }, { steps: 6, release: false })
      pushSessions()
      await tab.until("Clawdfather stays first while a finger holds the list", (s) =>
        s.order.join() === [RETAINED, SAFE].join())
      await tab.lift()
      await tab.go("/")
      await tab.until("Clawdfather leads after the held gesture", (s) => s.order.join() === [RETAINED, SAFE].join())
      await tab.press(`#rows > li.row[data-id="${SAFE}"]`)
      await tab.until("the waiting conversation opens on phone", (s) => s.detailName === "Needs an answer")
      await tab.desktop()
      await tab.go("/")
      await tab.until("Clawdfather leads on desktop", (s) => s.order.join() === [RETAINED, SAFE].join())
      assert.match((await tab.accessibilityOptions())[0] ?? "", /Clawdfather/)
      await tab.until("the waiting conversation opens beside the pinned list", (s) => s.open === SAFE)
      await tab.run(`location.hash = '#session=' + encodeURIComponent(${JSON.stringify(RETAINED)})`)
      await tab.until("the explicit Clawdfather address wins", (s) => s.open === RETAINED)
    } finally {
      readingScenario = "normal"
    }
  }))

test("an Epic owner's independent Root is visibly nested in the Session list", () =>
  inTab(async (tab) => {
    readingScenario = "epic"
    try {
      await tab.go("/")
      await tab.until("the Epic owner and Feature Roots arrive", (s) => s.order.join() === [SAFE, BLOCKED, UNKNOWN].join())
      await tab.until("the independent Root chip is drawn", (s) => s.taskVisible)
      const shown = await tab.run(`(() => {
        const owner = document.querySelector('#rows > li.row[data-id="${SAFE}"]')
        const feature = document.querySelector('#rows > li.row[data-id="${BLOCKED}"]')
        return {
          depth: feature?.dataset.depth,
          epic: feature?.dataset.epicParent,
          root: feature?.querySelector('.task-chip')?.textContent?.trim(),
          offset: feature?.getBoundingClientRect().left - owner?.getBoundingClientRect().left,
          through: feature?.dataset.treeThrough,
          trunk: feature ? getComputedStyle(feature.querySelector('.kid'), '::before').borderLeftStyle : null,
          elbow: feature ? getComputedStyle(feature.querySelector('.kid'), '::after').borderTopStyle : null,
          trunkWidth: feature ? getComputedStyle(feature.querySelector('.kid'), '::before').borderLeftWidth : null,
          elbowWidth: feature ? getComputedStyle(feature.querySelector('.kid'), '::after').borderTopWidth : null,
          opacity: feature ? getComputedStyle(feature.querySelector('.kid')).opacity : null,
          overflow: feature ? getComputedStyle(feature).overflowX : null,
        }
      })()`)
      assert.equal(shown.depth, "1")
      assert.equal(shown.epic, "epic-fixture")
      assert.ok(shown.root, JSON.stringify(shown))
      assert.ok(shown.offset >= 20, "the Feature Root is visibly indented under the Epic owner")
      assert.equal(shown.through, "1", "the first Root's trunk reaches the next Root")
      assert.equal(shown.trunk, "solid")
      assert.equal(shown.elbow, "solid")
      assert.equal(shown.trunkWidth, "1px")
      assert.equal(shown.elbowWidth, "1px")
      assert.equal(shown.opacity, "0.55")
      assert.equal(shown.overflow, "visible", "the connector is not clipped at the card edge")
      await tab.shot("epic-tree")
    } finally {
      readingScenario = "normal"
    }
  }))

test("the Board assignment picker explains a Session before assignment", () =>
  inTab(async (tab) => {
    readingScenario = "normal"
    await tab.go("/#page=work")
    const shown = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      let opened = false
      const read = () => {
        const summary = document.querySelector('#work [data-work-id="assignment-fixture"] .work-card-summary')
        if (summary && !document.querySelector('.work-item-detail-modal')) summary.click()
        const trigger = document.querySelector('.work-item-detail-modal [data-work-id="assignment-fixture"] .work-session-trigger')
        if (trigger && !opened) {
          opened = true
          trigger.click()
        }
        const options = [...document.querySelectorAll('.work-session-option')]
        const choice = options.find((option) => option.textContent.includes('Bravo is still working'))
        if (choice && choice.textContent.includes('1 看板 · 1 TODO')) {
          const menu = choice.textContent
          choice.click()
          const finish = () => {
            const detail = document.querySelector('.work-session-detail')
            const text = detail?.textContent || ''
            if (text.includes('Repair the previous release')) {
              // Opened again with a Session chosen, the menu still hangs from
              // the button, not from under the chosen Session's detail.
              const card = document.querySelector('.work-item-detail-modal [data-work-id="assignment-fixture"]')
              const again = card.querySelector('.work-session-trigger')
              again.click()
              return setTimeout(() => {
                const button = again.getBoundingClientRect()
                const list = card.querySelector('.work-session-menu')?.getBoundingClientRect()
                const progress = card.querySelector('.work-milestones')
                resolve({ menu, detail: text, gap: list ? list.top - button.bottom : null,
                  progressBeforeStart: !!progress })
              }, 50)
            }
            if (Date.now() >= deadline) return reject(new Error('the selected Session detail did not arrive: ' + text))
            setTimeout(finish, 25)
          }
          return finish()
        }
        if (Date.now() >= deadline) return reject(new Error('the informed Session choice did not arrive'))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.match(shown.menu, /Bravo is still working/)
    assert.match(shown.menu, /Working · 執行中/)
    assert.match(shown.menu, /1 看板 · 1 TODO/)
    assert.match(shown.detail, /尚未完成：1 個看板項目 · 1 個 TODO/)
    assert.match(shown.detail, /Coordinate the live deployment/)
    assert.match(shown.detail, /Review the release note/)
    assert.match(shown.detail, /Repair the previous release/)
    assert.ok(shown.gap !== null && Math.abs(shown.gap - 6) <= 1,
      `the reopened menu was ${shown.gap}px below its button, not 6`)
    assert.equal(shown.progressBeforeStart, false, "an unstarted item showed implementation progress")
    const startedProgress = await tab.run(`new Promise((resolve, reject) => {
      document.querySelector('.work-item-detail-modal .work-modal-close')?.click()
      const deadline = Date.now() + 8000
      const read = () => {
        const summary = document.querySelector('#work [data-work-id="header-fixture"] .work-card-summary')
        if (summary && !document.querySelector('.work-item-detail-modal')) summary.click()
        const progress = document.querySelector('.work-item-detail-modal [data-work-id="header-fixture"] .work-milestones')
        if (progress) return resolve(true)
        if (Date.now() >= deadline) return reject(new Error('the implementing item progress did not arrive'))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(startedProgress, true, "an implementing item lost its progress")
    await tab.shot("board-session-assignment")
  }))

test("a compact Board summary and its modal close icon are geometrically centered on a phone", () =>
  inTab(async (tab) => {
    await tab.go("/#page=work")
    const board = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const summary = document.querySelector('#work [data-work-id="assignment-fixture"] .work-card-summary')
        const openLabel = summary?.querySelector('.work-card-open')
        const openIcon = summary?.querySelector('.work-card-open .work-icon')
        const openLabelBox = openLabel?.getBoundingClientRect()
        const openBox = openIcon?.getBoundingClientRect()
        if (!openLabelBox || !openBox) {
          if (Date.now() >= deadline) return reject(new Error('the compact Board summary did not arrive'))
          return setTimeout(read, 25)
        }
        summary.click()
        setTimeout(() => {
          const button = document.querySelector('.work-item-detail-modal .work-modal-close')
          const icon = button?.querySelector('.work-icon')
          const buttonBox = button?.getBoundingClientRect()
          const iconBox = icon?.getBoundingClientRect()
          if (!buttonBox || !iconBox) return reject(new Error('the detail close control did not arrive'))
          resolve({ open: { tag: openIcon?.tagName || '',
              dy: Math.abs((openLabelBox.top + openLabelBox.height / 2) - (openBox.top + openBox.height / 2)) },
            close: { tag: icon?.tagName || '',
            dx: Math.abs((buttonBox.left + buttonBox.width / 2) - (iconBox.left + iconBox.width / 2)),
            dy: Math.abs((buttonBox.top + buttonBox.height / 2) - (iconBox.top + iconBox.height / 2)) } })
        }, 50)
      }
      read()
    })`)
    assert.equal(board.open.tag, "svg")
    assert.ok(board.open.dy <= 0.5, `Board open icon was ${board.open.dy}px off its vertical center`)
    assert.equal(board.close.tag, "svg")
    assert.ok(board.close.dx <= 0.5 && board.close.dy <= 0.5,
      `Board close icon was off center by ${JSON.stringify(board.close)}`)
    await tab.shot("board-icons-centered")
  }))

test("a folded Board card stays inside the phone beside a long Project name", () =>
  inTab(async (tab) => {
    await tab.go("/#page=work")
    const header = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const card = document.querySelector('#work [data-work-id="header-fixture"]')
        const summary = card?.querySelector('.work-card-summary')
        const description = card?.querySelector('.work-card-summary-description')
        if (!card || !summary || !description) {
          if (Date.now() >= deadline) return reject(new Error('the folded Board card did not arrive'))
          return setTimeout(read, 25)
        }
        resolve({
          width: document.documentElement.clientWidth,
          past: Math.max(0, card.scrollWidth - card.clientWidth),
          clamp: getComputedStyle(description).webkitLineClamp,
          inlineActions: card.querySelectorAll('.work-card-controls').length,
        })
      }
      read()
    })`)
    assert.equal(header.width, 390)
    assert.equal(header.past, 0, "the folded Board card ran past its edge")
    assert.equal(header.clamp, "2")
    assert.equal(header.inlineActions, 0, "full item actions were still expanded on the Board")
    await tab.shot("board-card-header")
  }))

test("a Board summary opens the detail with focus and Escape returns to that summary", () =>
  inTab(async (tab) => {
    await tab.go("/#page=work")
    const focus = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const open = () => {
        const summary = document.querySelector('#work [data-work-id="header-fixture"] .work-card-summary')
        if (!summary) {
          if (Date.now() >= deadline) return reject(new Error('the Board summary did not arrive'))
          return setTimeout(open, 25)
        }
        summary.focus()
        summary.click()
        setTimeout(() => {
          const modal = document.querySelector('.work-item-detail-modal')
          const close = modal?.querySelector('.work-modal-close')
          const openedOnClose = document.activeElement === close
          document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
          setTimeout(() => resolve({ openedOnClose, closed: !document.querySelector('.work-item-detail-modal'), returned: document.activeElement === summary,
            activeTag: document.activeElement?.tagName || '', activeClass: document.activeElement?.className || '', summaryConnected: summary.isConnected }), 25)
        }, 50)
      }
      open()
    })`)
    assert.deepEqual(focus, { openedOnClose: true, closed: true, returned: true,
      activeTag: "BUTTON", activeClass: "work-card-summary", summaryConnected: true })
  }))

test("a desktop Board keeps two-column summaries and a bounded detail modal", () =>
  inTab(async (tab) => {
    await tab.desktop()
    await tab.go("/#page=work")
    const layout = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const cards = document.querySelector('.work-cards')
        const summary = document.querySelector('#work [data-work-id="assignment-fixture"] .work-card-summary')
        if (!cards || !summary) {
          if (Date.now() >= deadline) return reject(new Error('the desktop Board did not arrive'))
          return setTimeout(read, 25)
        }
        const columns = getComputedStyle(cards).gridTemplateColumns.split(' ').filter(Boolean).length
        summary.click()
        setTimeout(() => {
          const panel = document.querySelector('.work-item-detail-panel')
          const box = panel?.getBoundingClientRect()
          if (!box) return reject(new Error('the desktop detail modal did not arrive'))
          resolve({ columns, width: Math.round(box.width), viewport: window.innerWidth,
            centered: Math.abs((box.left + box.width / 2) - window.innerWidth / 2) <= 1 })
        }, 50)
      }
      read()
    })`)
    assert.equal(layout.columns, 2)
    assert.ok(layout.width <= 720 && layout.width < layout.viewport)
    assert.equal(layout.centered, true)
    await tab.shot("board-desktop-detail")
  }))

test("the Session todo add icon is geometrically centered on a phone", () =>
  inTab(async (tab) => {
    await tab.go("/#session=" + encodeURIComponent(SAFE))
    const session = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const button = document.querySelector('.session-todos-add')
        const icon = button?.querySelector('.work-icon')
        const buttonBox = button?.getBoundingClientRect()
        const iconBox = icon?.getBoundingClientRect()
        if (!buttonBox || !iconBox) {
          if (Date.now() >= deadline) return reject(new Error('the Session todo add control did not arrive'))
          return setTimeout(read, 25)
        }
        resolve({ tag: icon?.tagName || '',
          dx: Math.abs((buttonBox.left + buttonBox.width / 2) - (iconBox.left + iconBox.width / 2)),
          dy: Math.abs((buttonBox.top + buttonBox.height / 2) - (iconBox.top + iconBox.height / 2)) })
      }
      read()
    })`)
    assert.equal(session.tag, "svg")
    assert.ok(session.dx <= 0.5 && session.dy <= 0.5,
      `Session todo add icon was off center by ${JSON.stringify(session)}`)
    await tab.shot("session-todo-add-centered")
  }))

test("a phone answers the question directly on its Session todo card with a visible receipt", () =>
  inTab(async (tab) => {
    inlineDecisionFixture = true
    inlineDecisionAnswers = 0
    try {
      await tab.go("/#session=" + encodeURIComponent(SAFE))
      const target = await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 8000
        const read = () => {
          const fold = document.querySelector('.session-todos')
          if (fold && !fold.open) fold.querySelector('summary')?.click()
          const question = document.querySelector('.session-owned-decision')
          const button = [...(question?.querySelectorAll('button') || [])].find((node) => node.textContent?.includes('Proceed'))
          if (button) {
            button.scrollIntoView({ block: 'center' })
            const rect = button.getBoundingClientRect()
            return resolve({ x: Math.round(rect.left + rect.width / 2), y: Math.round(rect.top + rect.height / 2),
              text: question?.textContent || '', width: rect.width, viewport: innerWidth })
          }
          if (Date.now() >= deadline) return reject(new Error('inline decision did not appear: ' + (document.body.textContent || '').slice(0, 700)))
          setTimeout(read, 25)
        }
        read()
      })`)
      assert.match(target.text, /Should the release proceed/)
      assert.ok(target.width <= target.viewport)
      await tab.shot("session-inline-decision-question")
      await tab.tap(target.x, target.y)
      const pending = await tab.run(`document.querySelector('.session-owned-decision')?.textContent || ''`)
      assert.match(pending, /正在送出.*Proceed/)
      const receipt = await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 8000
        const read = () => {
          const text = document.querySelector('.session-owned-item')?.textContent || ''
          if (text.includes('已回答：Proceed')) return resolve(text)
          if (Date.now() >= deadline) return reject(new Error('answer receipt did not appear: ' + text))
          setTimeout(read, 25)
        }
        read()
      })`)
      assert.match(receipt, /已回答：Proceed/)
      assert.equal(inlineDecisionAnswers, 1)
      await tab.shot("session-inline-decision-receipt")
    } finally {
      inlineDecisionFixture = false
      inlineDecisionAnswers = 0
    }
  }))

test("the Session to-do fold shows loading until its Board items arrive on a phone", () =>
  inTab(async (tab) => {
    delaySessionWork = true
    try {
      await tab.go("/#session=" + encodeURIComponent(SAFE))
      const pending = await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 8000
        const read = () => {
          const fold = document.querySelector('.session-todos')
          if (fold && !fold.open) fold.querySelector('summary')?.click()
          const loading = fold?.querySelector('.session-todos-loading[role="status"]')
          if (loading) return resolve({ text: loading.textContent,
            items: fold.querySelectorAll('.session-owned-item').length,
            animation: getComputedStyle(loading.querySelector('.work-assignment-spinner')).animationName })
          if (Date.now() >= deadline) return reject(new Error('the Session to-do loading state did not appear'))
          setTimeout(read, 25)
        }
        read()
      })`)
      assert.match(pending.text, /載入/)
      assert.equal(pending.items, 0)
      assert.equal(pending.animation, "work-assignment-spin")
      const loaded = await tab.run(`new Promise((resolve, reject) => {
        const deadline = Date.now() + 8000
        const read = () => {
          const fold = document.querySelector('.session-todos')
          const item = fold?.querySelector('.session-owned-item b')
          if (item?.textContent.includes('Finish the release receipt') && !fold.querySelector('.session-todos-loading')) {
            return resolve(item.textContent)
          }
          if (Date.now() >= deadline) return reject(new Error('the Session Board item did not replace loading'))
          setTimeout(read, 25)
        }
        read()
      })`)
      assert.match(loaded, /Finish the release receipt/)
    } finally {
      delaySessionWork = false
    }
  }))

test("an assigned Board item opens its detail and requested action on a phone", () =>
  inTab(async (tab) => {
    workReminders = 0
    await tab.go("/#session=" + encodeURIComponent(SAFE))
    const shown = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        const item = document.querySelector('.session-owned-summary')
        if (item && !document.querySelector('.work-item-detail-modal')) item.click()
        const modal = document.querySelector('.work-item-detail-modal')
        const text = modal?.textContent || ''
        if (text.includes('Confirm the production release window.')) {
          const panel = modal.querySelector('.work-item-detail-panel')
          const box = panel?.getBoundingClientRect()
          return resolve({ text, width: box?.width || 0, viewport: window.innerWidth,
            clientHeight: modal.clientHeight, scrollHeight: modal.scrollHeight,
            x: Math.round((box?.left || 0) + (box?.width || 0) / 2),
            y: Math.round(Math.min((box?.bottom || 0) - 80, window.innerHeight - 80)) })
        }
        if (Date.now() >= deadline) return reject(new Error('the Board item detail did not arrive: ' + text))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.match(shown.text, /Finish the release receipt/)
    assert.match(shown.text, /Publish the verified receipt/)
    assert.match(shown.text, /需要你做的事/)
    assert.ok(shown.width <= shown.viewport, `detail width ${shown.width} exceeds viewport ${shown.viewport}`)
    assert.ok(shown.scrollHeight > shown.clientHeight,
      `detail height ${shown.scrollHeight} did not exceed its ${shown.clientHeight}px viewport`)
    await tab.drag({ x: shown.x, y: shown.y }, { x: 0, y: -260 }, { steps: 12 })
    const scrolled = await tab.run(`(() => {
      const modal = document.querySelector('.work-item-detail-modal')
      const panel = document.querySelector('.work-item-detail-panel')
      return { modal: modal?.scrollTop || 0, panel: panel?.scrollTop || 0 }
    })()`)
    assert.ok(scrolled.modal > 0 || scrolled.panel > 0,
      `a real finger drag did not scroll the completion report: ${JSON.stringify(scrolled)}`)
    const reminded = await tab.run(`new Promise((resolve, reject) => {
      const button = [...document.querySelectorAll('.work-item-detail-modal button')]
        .find((node) => node.textContent?.includes('提醒 Session'))
      if (!button) return reject(new Error('the Session reminder button is missing'))
      button.click()
      const deadline = Date.now() + 8000
      const read = () => {
        const text = document.querySelector('.work-item-detail-modal')?.textContent || ''
        if (text.includes('已提醒')) return resolve(text)
        if (Date.now() >= deadline) return reject(new Error('the Session reminder did not settle: ' + text))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.match(reminded, /已提醒/)
    assert.equal(workReminders, 1)
    await tab.shot("session-board-item-detail")
  }))

test("a completed Board item is checked and its report scrolls with a real finger", () =>
  inTab(async (tab) => {
    await tab.go("/#session=" + encodeURIComponent(BLOCKED))
    const shown = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        const item = document.querySelector('.session-recent-work .session-owned-summary')
        const check = item?.querySelector('.session-owned-complete')
        const title = item?.querySelector('b')
        const activeTitle = document.querySelector('.session-owned-item:not(.completed) b')
        if (item && check && !document.querySelector('.work-item-detail-modal')) item.click()
        const modal = document.querySelector('.work-item-detail-modal')
        const panel = modal?.querySelector('.work-item-detail-panel')
        const body = modal?.querySelector('.work-completion-report-body')
        const heading = body?.querySelector('h2, h3, h4')
        const reportTitles = [...(modal?.querySelectorAll('.work-completion-report-title strong') || [])].map((node) => node.textContent)
        const reportTimes = [...(modal?.querySelectorAll('.work-completion-report-title time') || [])].map((node) => ({
          text: node.textContent, dateTime: node.getAttribute('datetime') }))
        const box = panel?.getBoundingClientRect()
        if (modal?.textContent.includes('Root cause') && box) return resolve({
          checked: check?.querySelector('.work-icon')?.tagName || '',
          titleColor: title ? getComputedStyle(title).color : '',
          activeTitleColor: activeTitle ? getComputedStyle(activeTitle).color : '',
          statusColor: getComputedStyle(check).color,
          parent: modal.parentElement === document.body,
          literalBreaks: modal.textContent.includes('\\\\n'),
          paragraphs: modal.querySelectorAll('.work-completion-report-body p').length,
          bodyColor: body ? getComputedStyle(body).color : '',
          bodyFontSize: body ? getComputedStyle(body).fontSize : '',
          headingColor: heading ? getComputedStyle(heading).color : '',
          headingFontSize: heading ? getComputedStyle(heading).fontSize : '',
          reportTitles,
          reportTimes,
          clientHeight: modal.clientHeight,
          scrollHeight: modal.scrollHeight,
          x: Math.round(box.left + box.width / 2),
          y: Math.round(Math.min(box.bottom - 80, window.innerHeight - 80)),
        })
        if (Date.now() >= deadline) return reject(new Error('the completed Board report did not arrive'))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(shown.checked, "svg")
    assert.equal(shown.titleColor, shown.activeTitleColor, "completion changed the item title colour")
    assert.notEqual(shown.titleColor, shown.statusColor, "the title used the completion status colour")
    assert.equal(shown.parent, true, "the modal stayed nested inside the fixed Session pane")
    assert.equal(shown.literalBreaks, false, "legacy paragraph separators were shown as literal \\n text")
    assert.ok(shown.paragraphs > 20, `the completion report rendered only ${shown.paragraphs} paragraphs`)
    assert.equal(shown.bodyColor, "rgb(232, 230, 227)", "completion prose did not use the high-contrast ink colour")
    assert.equal(shown.bodyFontSize, "15px", "completion prose stayed too small on a phone")
    assert.equal(shown.headingColor, "rgb(232, 230, 227)", "completion subhead did not use the high-contrast ink colour")
    assert.equal(shown.headingFontSize, "16px", "completion subhead stayed too small on a phone")
    assert.deepEqual(shown.reportTitles, ["Newest completion report", "Older completion report"],
      "the report history did not put the latest written report first")
    assert.equal(shown.reportTimes[0]?.dateTime, new Date(200_000).toISOString(), "the newest report lost its write timestamp")
    assert.match(shown.reportTimes[0]?.text || "", /^寫於 /, "the report timestamp was not labelled for the reader")
    assert.ok(shown.scrollHeight > shown.clientHeight,
      `detail height ${shown.scrollHeight} did not exceed its ${shown.clientHeight}px viewport`)
    await tab.drag({ x: shown.x, y: shown.y }, { x: 0, y: -260 }, { steps: 12 })
    const scrollTop = await tab.run(`document.querySelector('.work-item-detail-modal')?.scrollTop || 0`)
    assert.ok(scrollTop > 0, `a real finger drag left the completion report at ${scrollTop}`)
    await tab.shot("session-completed-board-report")
  }))

test("a direct todo attachment is a compact clickable filename on a phone", () =>
  inTab(async (tab) => {
    await tab.go("/#session=" + encodeURIComponent(BLOCKED))
    const attachment = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const fold = document.querySelector('.session-todos')
        if (fold && !fold.open) fold.querySelector('summary')?.click()
        const link = document.querySelector('.session-todo-image-link')
        const box = link?.getBoundingClientRect()
        if (link?.getAttribute('href')?.startsWith('blob:') && box) return resolve({
          tag: link.tagName,
          text: link.textContent || '',
          target: link.getAttribute('target'),
          imageCount: link.querySelectorAll('img').length,
          width: box.width,
          height: box.height,
        })
        if (Date.now() >= deadline) return reject(new Error('the compact todo attachment did not arrive'))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.equal(attachment.tag, "A")
    assert.match(attachment.text, /release-evidence\.png/)
    assert.equal(attachment.target, "_blank")
    assert.equal(attachment.imageCount, 0, "the compact attachment still rendered a thumbnail")
    assert.ok(attachment.width > attachment.height * 4,
      `attachment ${attachment.width}x${attachment.height} was not a horizontal row`)
    assert.ok(attachment.height <= 52, `attachment row grew to ${attachment.height}px`)
    await tab.shot("session-todo-compact-attachment")
  }))

test("an expanded Session todo fold shades the conversation and a backdrop tap closes it on a phone", () =>
  inTab(async (tab) => {
    await tab.go("/#session=" + encodeURIComponent(BLOCKED))
    const openVisuals = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const open = () => {
        const fold = document.querySelector('.session-todos')
        const body = fold?.querySelector('.session-todos-body')
        const backdrop = fold?.querySelector('.session-todos-backdrop')
        const transcript = document.querySelector('.tx-scroll')
        if (!fold || !body || !backdrop || !transcript) {
          if (Date.now() >= deadline) return reject(new Error('the Session todo fold did not arrive'))
          return setTimeout(open, 25)
        }
        if (!fold.open) fold.querySelector('summary')?.click()
        setTimeout(() => {
          const panel = getComputedStyle(fold)
          const veil = getComputedStyle(fold, '::after')
          const content = getComputedStyle(body)
          const foldBox = fold.getBoundingClientRect()
          const veilBox = backdrop.getBoundingClientRect()
          const transcriptBox = transcript.getBoundingClientRect()
          const veilX = Math.round(veilBox.left + veilBox.width / 2)
          const veilY = Math.round(Math.min(window.innerHeight - 30, veilBox.top + 60))
          resolve({
            open: fold.open,
            shadow: panel.boxShadow,
            veilOpacity: veil.opacity,
            veilFilter: veil.backdropFilter || veil.webkitBackdropFilter || '',
            veilTop: Number.parseFloat(veil.top),
            veilX,
            veilY,
            hitClass: document.elementFromPoint(veilX, veilY)?.className || '',
            foldHeight: foldBox.height,
            foldBottom: foldBox.bottom,
            transcriptTop: transcriptBox.top,
            bodyAnimation: content.animationName,
          })
        }, 220)
      }
      open()
    })`)
    assert.equal(openVisuals.open, true)
    assert.notEqual(openVisuals.shadow, "none")
    assert.equal(openVisuals.veilOpacity, "1")
    assert.match(openVisuals.veilFilter, /blur\(7px\)/)
    assert.match(openVisuals.hitClass, /session-todos-backdrop/)
    assert.match(openVisuals.bodyAnimation, /session-todos-body-in/)
    assert.ok(Math.abs(openVisuals.veilTop - openVisuals.foldHeight) <= 2,
      `veil began at ${openVisuals.veilTop}px instead of the ${openVisuals.foldHeight}px fold edge`)
    assert.ok(openVisuals.transcriptTop <= openVisuals.foldBottom + 2,
      `conversation began at ${openVisuals.transcriptTop}px beyond the ${openVisuals.foldBottom}px fold edge`)
    await tab.shot("session-todos-open-glass-layer")

    await tab.tap(openVisuals.veilX, openVisuals.veilY)
    const closed = await tab.run(`new Promise((resolve) => {
      const fold = document.querySelector('.session-todos')
      setTimeout(() => resolve({
        open: fold?.open,
        veilOpacity: fold ? getComputedStyle(fold, '::after').opacity : '',
      }), 220)
    })`)
    assert.equal(closed.open, false)
    assert.equal(closed.veilOpacity, "0")
  }))

test("a Project-page Board address selects and reads that Project", () =>
  inTab(async (tab) => {
    workProjectReads = []
    await tab.go("/#page=work&project=%2Ftmp%2Ffixture&from=projects")
    const selected = await tab.run(`new Promise((resolve, reject) => {
      const deadline = Date.now() + 8000
      const read = () => {
        const text = document.querySelector('.work-project-trigger')?.textContent || ''
        if (text.includes('Clawdline')) return resolve(text)
        if (Date.now() >= deadline) return reject(new Error('the Project-scoped Board did not arrive: ' + text))
        setTimeout(read, 25)
      }
      read()
    })`)
    assert.match(selected, /Clawdline/)
    assert.ok(workProjectReads.includes("project-fixture"))
  }))

// ---- the two gestures that were here first

test("opening and idling on the list takes one HTTP snapshot while the stream stays open", () =>
  inTab(async (tab) => {
    const before = listReads
    await list(tab)
    assert.equal(listReads - before, 1, "opening the list reads one snapshot")
    assert.equal(streams.size, 1, "one stream carries later updates")
    await new Promise((resolve) => setTimeout(resolve, 2500))
    assert.equal(listReads - before, 1, "idle stream updates do not request another full snapshot")
  }))

test("pull to refresh: a pull at the top reads the list again and says so on the way", () =>
  inTab(async (tab) => {
    await list(tab)
    const before = listReads
    const from = await tab.centreOf(SAFE)
    // Held at the bottom of the pull so the word can be read before the
    // finger lifts: past 62px the pad says "let go and it refreshes".
    await tab.drag({ x: from.x, y: 160 }, { x: 0, y: 220 }, { steps: 10, hold: 0, release: false })
    const pulled = await tab.seen()
    assert.equal(pulled.ptrWord, "放開就重新整理", "the pad asks to be released past the threshold")
    assert.ok(parseFloat(pulled.ptrHeight) > 0, "the pad has opened; it showed " + JSON.stringify(pulled.ptrHeight))
    await tab.lift()
    await tab.until("the list is read again", () => listReads > before)
    await tab.until("the pad closes again", (s) => s.ptrHeight === "0px" && s.ptrWord === "下拉重新整理", 4000)
  }))

test("the order is held: a finger on the list stops it re-sorting under itself", () =>
  inTab(async (tab) => {
    await list(tab)
    const from = await tab.centreOf(SAFE)
    // The finger goes down and stays down, and drags across the row — the
    // gesture the swipe is made of. While it is there the daemon pushes a list
    // in which one row has moved up; the rows must not move under the finger.
    await tab.drag(from, { x: -90, y: 0 }, { steps: 6, release: false })
    spareMoved = MOVED.spareAfter
    pushSessions()
    // Long enough for a redraw to have happened if one were going to: the
    // list is rebuilt on every frame that arrives.
    await new Promise((r) => setTimeout(r, 500))
    const held = await tab.seen()
    assert.equal(held.order.join(), ORDER_AT_REST.join(), "the order is held while the finger is down")
    // And the proof that the frame really did arrive: once the finger has been
    // gone, and nothing is left uncovered for it to hold the list still for,
    // the list takes the new order.
    await tab.lift()
    await tab.drag({ x: from.x - 90, y: from.y }, { x: 110, y: 0 }, { steps: 6 })
    await tab.until(
      "the order is let go once the finger has been off the list for a moment",
      (s) => s.order.join() === ORDER_ONCE_SPARE_MOVED.join(),
      6000,
    )
  }))

test("rows travel to a changed sorted position instead of snapping there", () =>
  inTab(async (tab) => {
    await list(tab)
    spareMoved = MOVED.spareAfter
    pushSessions()
    const moving = await tab.until(
      "the changed order is visible while its rows are moving",
      (s) => s.order.join() === ORDER_ONCE_SPARE_MOVED.join() && s.moving,
    )
    assert.equal(moving.moving, true)
    await tab.until("the rows reach their new resting positions", (s) => !s.moving)
  }))

// ---- the swipe

/** A left swipe on one row, finished. */
async function swipeOpen(tab: Tab, id: string): Promise<Seen> {
  const from = await tab.centreOf(id)
  await tab.drag(from, { x: -130, y: 0 }, { steps: 8 })
  return tab.until("the row's action is uncovered", (s) => s.swiping === id && s.swipeState === "open")
}

test("a left swipe uncovers a close, and the swipe itself closes nothing", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, SAFE)
    assert.equal(open.action, "關閉 Session", "the uncovered control says what pressing it does")
    assert.equal(open.actionKind, "safe")
    // The row names its conversation, so it uncovers 封存 beside the close:
    // two 88px buttons (docs/session-archive.md, `archive.e2e.ts`).
    assert.equal(open.swipeX, "-176px", "and the row's contents have moved out of its way")
    await tab.shot("swipe-safe")
    // The whole point: the gesture is not the decision.
    assert.deepEqual(closes, [], "nothing has been closed by the gesture")
    assert.equal(open.sheet, null, "and nothing has been asked yet either")
  }))

test("pressing the uncovered close asks first, naming the row, with Cancel under the focus", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, SAFE)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Alpha is finished 嗎？", "it names the row it would act on")
    assert.equal(asked.focused, "action-confirm-cancel", "and opens on the answer that changes nothing")
    const work = await tab.until("the unfinished Board item is named", (s) =>
      s.confirmDisabled === false && (s.sheetSay ?? "").includes("Finish the release receipt"))
    assert.match(work.sheetSay ?? "", /還有未完成項目/)
    assert.match(work.sheetSay ?? "", /看板\s*Finish the release receipt/)
    assert.doesNotMatch(work.sheetSay ?? "", /Clawdline 還不能確認/)
    assert.equal(work.completedSummary, null, "nothing completed draws no green summary")
    assert.doesNotMatch(work.sheetSay ?? "", /已完成 0 個看板項目/)
    await tab.shot("swipe-confirm")
    assert.deepEqual(closes, [], "still nothing closed while the question stands")
  }))

test("the second press is what closes it, once, under a key a retry can be answered with", () =>
  inTab(async (tab) => {
    readingScenario = "safe-cleared"
    try {
      await list(tab)
      const open = await swipeOpen(tab, SAFE)
      assert.equal(open.actionKind, "safe", "the Go safe reading needs no retired attestation")
      assert.doesNotMatch(open.rowState ?? "", /可以安全關閉/, "the confirmation, not the row, says it")
      await tab.press("li.row[data-swipe='open'] > .swipe-end")
      const asked = await tab.until("the confirmation has checked Board work", (s) => s.sheet !== null && s.confirmDisabled === false)
      assert.match(asked.sheetSay ?? "", /Clawdline 已確認這個 Session 可以安全關閉/)
      await tab.press("#action-confirm-go")
      await tab.until("the close has been asked for", () => closes.length > 0)
      assert.equal(closes.length, 1, "one press, one close")
      assert.equal(closes[0].id, SAFE)
      assert.equal(closes[0].force, false, "the first decision is not forced")
      assert.equal(closes[0].version, "cl1_fixture", "the decision carries the version displayed when the sheet opened")
      assert.ok(closes[0].key.length > 0, "under an Idempotency-Key, so a lost answer is not a second close")
    } finally {
      readingScenario = "normal"
    }
  }))

test("an unstarted Codex can be confirmed and closed without a conversation work read", () =>
  inTab(async (tab) => {
    sessionWorkReads = 0
    try {
      await list(tab)
      readingScenario = "unstarted-codex"
      pushSessions()
      await tab.until("only the empty Codex row remains", (s) => s.order.join() === SAFE)
      const open = await swipeOpen(tab, SAFE)
      assert.equal(open.actionKind, "safe")
      await tab.press("li.row[data-swipe='open'] > .swipe-end")
      const asked = await tab.until("the empty Codex is safe to close", (s) =>
        s.sheet !== null && s.confirmDisabled === false)
      assert.match(asked.sheetSay ?? "", /Clawdline 已確認這個 Session 可以安全關閉/)
      assert.equal(sessionWorkReads, 0, "an unstarted conversation has no conversation work to request")
      await tab.press("#action-confirm-go")
      await tab.until("the empty Codex close reaches the daemon", () => closes.length > 0)
      assert.equal(closes[0].id, SAFE)
      assert.equal(closes[0].force, false)
    } finally {
      readingScenario = "normal"
    }
  }))

test("a row with something still owed still uncovers the close action", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, BLOCKED)
    assert.equal(open.actionKind, "blocked")
    assert.equal(open.action, "關閉 Session", "the cell says what pressing it does, not why the close is blocked")
    assert.doesNotMatch(open.rowState ?? "", /項未了結/, "closeability is stated in the confirmation, not on the row")
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const opened = await tab.until("the blocked confirmation is up", (s) => s.sheet !== null)
    const asked = await tab.until("the blocked confirmation has checked Session work", (s) =>
      s.confirmDisabled === false && (s.sheetSay ?? "").includes("已完成 1 個看板項目、0 個 TODO"))
    assert.equal(opened.sheet, "要關閉 Bravo is still working 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/, "it says what closing does")
    assert.match(asked.sheetSay ?? "", /已完成 1 個看板項目、0 個 TODO/, "it summarizes completed work without dumping titles")
    assert.match(asked.sheetSay ?? "", /還有未完成項目/)
    assert.match(asked.sheetSay ?? "", /看板\s*Coordinate the live deployment/)
    assert.match(asked.sheetSay ?? "", /TODO\s*Review the release note/)
    assert.match(asked.sheetSay ?? "", /Agent 還在這個 session 工作；等這一輪結束。/)
    assert.doesNotMatch(asked.sheetSay ?? "", /Repair the previous release|terminal_working/)
    assert.match(asked.technicalText ?? "", /terminal_working/, "searchable broker details stay available behind disclosure")
    assert.equal(asked.technicalOpen, false, "technical details do not compete with the decision")
    await tab.shot("swipe-blocked")
  }))

test("a refused close explains what is owed, then still close overrides that disclosed obligation", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, BLOCKED)
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    await tab.until("the first confirmation has checked Board work", (s) => s.sheet !== null && s.confirmDisabled === false)
    await tab.press("#action-confirm-go")
    await tab.until("the daemon's refusal is shown", (s) => s.sheet !== null && s.confirmAction === "仍要關閉" && s.confirmDisabled === false)
    assert.equal(closes.length, 1)
    assert.equal(closes[0].force, false, "the close gate gets the first decision")
    const said = await tab.seen()
    assert.doesNotMatch(said.sheetSay ?? "", /terminal_working/, "the main reminder never falls back to broker vocabulary")
    assert.match(said.technicalText ?? "", /terminal_working/, "the daemon's reason remains in technical details")

    await tab.press("#action-confirm-go")
    await tab.until("the override reaches the daemon", () => closes.length === 2)
    assert.equal(closes[1].force, true, "only the explicit still-close decision overrides the gate")
    assert.equal(closes[1].version, closes[0].version, "the override stays bound to the displayed decision")
    assert.notEqual(closes[1].key, closes[0].key, "the override is a new decision, not a retry")
  }))

test("an unknown row keeps saying unknown while its swipe remains an action", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, UNKNOWN)
    assert.equal(open.actionKind, "unknown")
    assert.equal(open.action, "關閉 Session")
    assert.doesNotMatch(open.rowState ?? "", /無法判斷能否關閉/, "closeability is stated in the confirmation, not on the row")
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const opened = await tab.until("the unknown confirmation is up", (s) => s.sheet !== null)
    const asked = await tab.until("the unknown confirmation has checked Session work", (s) =>
      s.confirmDisabled === false && (s.sheetSay ?? "").includes("Clawdline 暫時無法確認最新的 session 資料"))
    assert.equal(opened.sheet, "要關閉 Charlie cannot be read 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/)
    assert.match(asked.sheetSay ?? "", /Clawdline 暫時無法確認最新的 session 資料/)
    assert.doesNotMatch(asked.sheetSay ?? "", /session_identity_ambiguous/)
    assert.match(asked.technicalText ?? "", /session_identity_ambiguous/)
    await tab.shot("swipe-unknown")
  }))

test("a brief inventory refresh leaves the row without a closeability badge, at the same height, while close stays unknown", () =>
  inTab(async (tab) => {
    await list(tab)
    const measure = () => tab.run(`(() => {
      const row = document.querySelector("#rows > li.row[data-id='${BLOCKED}']")
      return {
        height: row.getBoundingClientRect().height,
        top: row.getBoundingClientRect().top,
        state: row.querySelector(".state").textContent,
        badge: row.querySelector(".session-closeability")?.dataset.closeability ?? null,
      }
    })()`)
    const before = await measure()
    assert.equal(before.badge, null, "the list draws no closeability badge")
    readingScenario = "refresh"
    try {
      pushSessions()
      await tab.until("the retained reading arrived", (s) => s.refreshing)
      const during = await measure()
      assert.equal(during.badge, null)
      assert.doesNotMatch(during.state, /項未了結|無法判斷能否關閉/)
      assert.equal(during.height, before.height)
      assert.equal(during.top, before.top)
      assert.equal((await tab.seen()).readingBanner, "", "a routine refresh does not add a list banner")
      const opened = await swipeOpen(tab, BLOCKED)
      assert.equal(opened.actionKind, "unknown", "a retained reading does not authorize close")
    } finally {
      readingScenario = "normal"
      pushSessions()
    }
  }))

test("a routine refresh keeps close pending without calling the row verification paused", () =>
  inTab(async (tab) => {
    await list(tab)
    readingScenario = "refresh"
    try {
      pushSessions()
      await tab.until("the retained reading arrived", (s) => s.refreshing)
      const open = await swipeOpen(tab, SAFE)
      assert.equal(open.actionKind, "unknown", "a prior safe answer cannot authorize close")
      assert.doesNotMatch(open.rowState ?? "", /驗證暫停/)
      assert.doesNotMatch(open.rowState ?? "", /無法判斷能否關閉|可安全關閉/)
    } finally {
      readingScenario = "normal"
      pushSessions()
    }
    await tab.until("the complete reading arrived", (s) => !s.refreshing)
    const restored = await swipeOpen(tab, SAFE)
    assert.equal(restored.actionKind, "safe", "a complete new reading restores the close action")
  }))

test("a Claude process without a first conversation stays listed without a false paused warning", () =>
  inTab(async (tab) => {
    await list(tab)
    readingScenario = "unstarted-claude-refresh"
    try {
      pushSessions()
      await tab.until("the unstarted Claude row arrives", (s) => s.order.join() === SAFE && s.refreshing)
      const open = await swipeOpen(tab, SAFE)
      assert.match(open.rowState ?? "", /對話還沒開始/)
      assert.doesNotMatch(open.rowState ?? "", /驗證暫停/)
    } finally {
      readingScenario = "normal"
      pushSessions()
    }
  }))

test("a session awaiting attestation explains that inside its named confirmation", () =>
  inTab(async (tab) => {
    await list(tab)
    const open = await swipeOpen(tab, NEEDS_ATTESTATION)
    assert.equal(open.actionKind, "needs_attestation")
    assert.equal(open.action, "關閉 Session")
    assert.doesNotMatch(open.rowState ?? "", /等這個 session 自己確認/, "the confirmation, not the row, says it")
    await tab.press("li.row[data-swipe='open'] > .swipe-end")
    const asked = await tab.until("the attestation confirmation is up", (s) => s.sheet !== null)
    assert.equal(asked.sheet, "要關閉 Echo has not checked in 嗎？")
    assert.match(asked.sheetSay ?? "", /Agent 會先結束，接著關閉它的終端機分頁。/)
    const ready = await tab.until("the completed work and safe-close reminder are shown", (s) =>
      s.confirmDisabled === false && (s.sheetSay ?? "").includes("已完成 1 個看板項目、1 個 TODO"))
    assert.equal(ready.completedSummary?.trim(), "✓已完成 1 個看板項目、1 個 TODO")
    assert.equal(ready.completedMarkColor, "rgb(95, 158, 115)", "the completed mark uses the success colour")
    assert.doesNotMatch(ready.sheetSay ?? "", /Repair the previous release|Verify the hosted console/)
    assert.match(ready.sheetSay ?? "", /沒有未完成的看板項目或 TODO。/)
    assert.match(ready.sheetSay ?? "", /現在可以安全關閉這個 Session。/)
    assert.equal(ready.readyStatusColor, "rgb(232, 230, 227)", "the safe-close sentence uses the main text colour")
    assert.equal(ready.readyMarkColor, "rgb(95, 158, 115)", "only the safe-close mark uses the success colour")
    assert.equal(ready.confirmAction, "安全關閉")
    await tab.shot("swipe-ready-to-close")
  }))

test("one row is uncovered at a time, and the press that puts one away opens no session", () =>
  inTab(async (tab) => {
    await list(tab)
    await swipeOpen(tab, SAFE)
    const other = await tab.centreOf(UNKNOWN)
    await tab.tap(other.x, other.y)
    const after = await tab.until("the uncovered action is put away", (s) => s.swiping === null)
    assert.equal(after.sheet, null)
    assert.equal(
      await tab.run(`document.querySelectorAll("#rows > li.row.open").length`),
      0,
      "and that press did not open a session either",
    )
  }))

test("a diagonal drag belongs to one gesture: the pad and the row never move together", () =>
  inTab(async (tab) => {
    await list(tab)
    const from = await tab.centreOf(SAFE)
    // Mostly down, a little left: the scroller's, so the pad opens and the row
    // stays where it is.
    await tab.drag({ x: from.x, y: 150 }, { x: -70, y: 200 }, { steps: 10, release: false })
    const pulling = await tab.seen()
    assert.ok(parseFloat(pulling.ptrHeight) > 0, "the pull is the pull")
    assert.equal(pulling.swiping, null, "and the row did not come with it")
    await tab.lift()
    await tab.until("the pad closes again", (s) => s.ptrHeight === "0px", 4000)
    // Mostly left, a little down: the row's, so the row moves and the pad
    // stays shut.
    const row = await tab.centreOf(SAFE)
    await tab.drag(row, { x: -130, y: 30 }, { steps: 10, release: false })
    const swiping = await tab.seen()
    assert.equal(swiping.swiping, SAFE, "the swipe is the swipe")
    assert.equal(swiping.swipeState, "dragging")
    assert.equal(parseFloat(swiping.ptrHeight || "0"), 0, "and the pad did not come with it")
    await tab.lift()
  }))

// ---- retained-reading age and phone layout

test("the densest phone row gives each segment a boundary and never widens the list", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until(
        "the worst-case row and its task arrive",
        (s) => s.order.join() === RETAINED && s.taskVisible,
      )
      const measured = await tab.run(`(() => {
        const scroller = document.querySelector(".list-scroll")
        const row = document.querySelector("#rows > li.row")
        const meta = row.querySelector(".meta")
        const state = row.querySelector(".state")
        const shown = (selector) => getComputedStyle(row.querySelector(selector)).display !== "none"
        const box = row.getBoundingClientRect()
        const rail = scroller.getBoundingClientRect()
        return {
          viewport: [innerWidth, innerHeight],
          list: [scroller.clientWidth, scroller.scrollWidth],
          row: [row.clientWidth, row.scrollWidth, box.left, box.right, rail.left, rail.right],
          meta: [meta.clientWidth, meta.scrollWidth],
          state: [state.clientWidth, state.scrollWidth],
          machine: !!row.querySelector(".machine"),
          path: shown(".path"),
          coordinator: shown(".coordinator-chip"),
          agents: shown(".agents-chip"),
          task: shown(".task-chip"),
        }
      })()`)
      assert.deepEqual(measured.viewport, [390, 844])
      assert.equal(measured.list[1], measured.list[0], "the list has no horizontal overflow")
      assert.ok(measured.row[3] <= measured.row[5], "the card ends inside the list rail: " + JSON.stringify(measured.row))
      assert.ok(measured.meta[1] <= measured.meta[0], "the metadata segments converge inside their line")
      assert.ok(measured.state[1] <= measured.state[0], "the state segments converge inside their line")
      assert.equal(measured.machine, false, "a single-machine list does not repeat its machine on every row")
      assert.equal(measured.path, false, "the repeated path is first to leave the phone row")
      assert.equal(measured.coordinator, false, "the crown keeps the role when its duplicate word leaves")
      assert.equal(measured.agents, true)
      assert.equal(measured.task, true)
      await tab.shot("list-overflow-after")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a phone working row shows live text unless another state segment crowds it", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until("the working row arrives", (s) => s.order.join() === RETAINED)
      const measure = () => tab.run(`(() => {
        const state = document.querySelector("#rows > li.row .state")
        const line = state.querySelector(".session-live-line")
        return {
          text: line?.textContent,
          title: line?.getAttribute("title"),
          visible: line && getComputedStyle(line).display !== "none",
          spinner: !!state.querySelector("canvas.spin"),
          width: state.getBoundingClientRect().width,
        }
      })()`)
      const sparse = await measure()
      assert.ok(sparse.width < 390, "the phone state rail is narrow")
      assert.equal(sparse.visible, true, "a working phone row shows its live text")
      assert.equal(sparse.spinner, true)
      assert.match(sparse.text, /^Gitifying every package/)

      readingScenario = "live-crowded"
      await tab.go("/")
      await tab.until("the crowded working row arrives", (s) => s.order.join() === RETAINED)
      const crowded = await measure()
      assert.equal(crowded.visible, false, "the shell status takes priority on a crowded phone row")
      assert.equal(crowded.spinner, true)

      await tab.desktop()
      const wide = await measure()
      assert.ok(wide.width > 0, "the desktop state rail is laid out")
      assert.equal(wide.visible, true)
      assert.equal(wide.title, wide.text)
      assert.equal(wide.spinner, true)
    } finally {
      readingScenario = "normal"
    }
  }))

// Closeability is not a list axis, so "status-two" (delivery + closeability)
// draws the same single segment as "status-one" and is not measured twice.
for (const [scenario, segments] of [
  ["status-one", 1],
  ["status-three", 2],
] as const) {
  test(`${segments} status segment${segments === 1 ? "" : "s"} share the 390x844 row without an internal hole`, () =>
    inTab(async (tab) => {
      readingScenario = scenario
      try {
        await tab.go("/")
        await tab.until("the status-density row arrives", (s) => s.order.join() === RETAINED)
        const measured = await tab.run(`(() => {
          const state = document.querySelector("#rows > li.row .state")
          const completion = state.querySelector(":scope > .session-work-completion")
          const copy = completion.querySelector(".session-work-copy")
          const direct = [...state.children]
          const box = (node) => {
            const rect = node.getBoundingClientRect()
            const style = getComputedStyle(node)
            return {
              left: rect.left,
              right: rect.right,
              width: rect.width,
              clientWidth: node.clientWidth,
              scrollWidth: node.scrollWidth,
              overflow: style.overflow,
              textOverflow: style.textOverflow,
            }
          }
          return {
            viewport: [innerWidth, innerHeight],
            state: box(state),
            completion: box(completion),
            copy: box(copy),
            direct: direct.map((node) => ({ cls: node.className, ...box(node) })),
          }
        })()`)
        assert.deepEqual(measured.viewport, [390, 844])
        assert.equal(measured.direct.length, segments, "the fixture draws the requested number of independent status axes")
        assert.ok(measured.state.scrollWidth <= measured.state.clientWidth, "the complete state rail fits its row")
        assert.ok(
          Math.abs(measured.completion.right - measured.copy.right) <= 1,
          "the delivered sentence reaches its own boundary instead of leaving empty space inside it: " + JSON.stringify(measured),
        )
        assert.equal(measured.copy.textOverflow, "ellipsis", "the delivered sentence owns its ellipsis")
        for (const part of measured.direct) {
          assert.ok(part.right <= measured.state.right + 1, "each status segment ends inside the state rail: " + JSON.stringify(part))
        }
        await tab.shot(scenario)
      } finally {
        readingScenario = "normal"
      }
    }))
}

test("a reported turn says awaiting acceptance only for an unopened deployed item, and unknown when unread", () =>
  inTab(async (tab) => {
    readingScenario = "acceptance"
    type Seen = { id: string; acceptance: string; text: string; fits: boolean }
    const measure = async (): Promise<{ rows: Seen[]; pageFits: boolean; width: number }> => tab.run(`(() => ({
      width: innerWidth,
      pageFits: document.documentElement.scrollWidth <= innerWidth,
      rows: [...document.querySelectorAll("#rows > li.row")].map((li) => {
        const completion = li.querySelector(".session-work-completion")
        const copy = completion && completion.querySelector(".session-work-copy")
        const state = li.querySelector(".state")
        return {
          id: li.dataset.id,
          acceptance: completion && completion.dataset.acceptance,
          text: copy && copy.textContent,
          fits: !!state && state.scrollWidth <= state.clientWidth,
        }
      }),
    }))()`)
    const check = (measured: { rows: Seen[]; pageFits: boolean; width: number }) => {
      const of = (id: string) => measured.rows.find((m) => m.id === id) as Seen
      const by = { pending: of(SAFE), none: of(BLOCKED), unknown: of(UNKNOWN), older: of(SPARE) }
      assert.deepEqual([by.pending.acceptance, by.none.acceptance, by.unknown.acceptance, by.older.acceptance],
        ["pending", "none", "unknown", "unknown"], JSON.stringify(measured))
      assert.match(by.pending.text, /A deployed Board item/, "the awaiting item is named")
      assert.doesNotMatch(by.none.text, /A deployed Board item/, "a report with nothing awaiting names no item")
      assert.notEqual(by.unknown.text, by.none.text, "an unread Board is not shown as nothing awaiting")
      assert.equal(by.older.text, by.unknown.text, "a report without the field reads as unread, not as none")
      assert.ok(measured.pageFits, `the page does not scroll sideways at ${measured.width}px`)
      for (const m of measured.rows) assert.ok(m.fits, `the receipt stays inside its ${measured.width}px row: ` + JSON.stringify(m))
    }
    try {
      await tab.go("/")
      await tab.until("the four reported rows arrive", (s) => s.order.length === 4)
      const phone = await measure()
      assert.equal(phone.width, 390)
      check(phone)
      await tab.shot("acceptance")
      await tab.desktop()
      await tab.go("/")
      await tab.until("the four reported rows arrive on a desktop", (s) => s.order.length === 4)
      const desktop = await measure()
      assert.equal(desktop.width, 1280)
      check(desktop)
      await tab.shot("acceptance-desktop")
    } finally {
      readingScenario = "normal"
    }
  }))

test("the first visible phone count has no separator and owns its ellipsis", () =>
  inTab(async (tab) => {
    readingScenario = "worst"
    try {
      await tab.go("/")
      await tab.until("the working count arrives", (s) => s.order.join() === RETAINED)
      const measured = await tab.run(`(() => {
        const shown = [...document.querySelectorAll("#counts > .part")]
          .filter((node) => getComputedStyle(node).display !== "none")
        const first = shown[0]
        const style = getComputedStyle(first)
        const before = getComputedStyle(first, "::before")
        return {
          text: first.textContent,
          separator: before.content,
          textOverflow: style.textOverflow,
          clientWidth: first.clientWidth,
          scrollWidth: first.scrollWidth,
        }
      })()`)
      assert.equal(measured.text, "1 個在跑")
      assert.ok(measured.separator === "none" || measured.separator === "normal" || measured.separator === "\"\"")
      assert.equal(measured.textOverflow, "ellipsis", "the visible count owns its ellipsis instead of relying on header clipping")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a five-second retained reading has no row-level age note at 390x844", () =>
  inTab(async (tab) => {
    readingScenario = "five"
    try {
      await tab.go("/")
      const seen = await tab.until("the retained row arrives without an age note", (s) => s.order.join() === RETAINED)
      assert.equal(seen.retainedText, null)
      await tab.shot("reading-5-seconds")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a ninety-second retained reading is a quiet, one-line annotation at 390x844", () =>
  inTab(async (tab) => {
    readingScenario = "ninety"
    try {
      await tab.go("/")
      const seen = await tab.until("the older retained row arrives with its age note", (s) => s.retainedText !== null)
      assert.equal(seen.retainedText, "1 分鐘前沒有新輸出")
      const measured = await tab.run(`(() => {
        const note = document.querySelector(".retained-reading")
        if (!note) throw new Error("no retained note")
        const style = getComputedStyle(note)
        const box = note.getBoundingClientRect()
        return {
          fontSize: style.fontSize,
          lineHeight: style.lineHeight,
          whiteSpace: style.whiteSpace,
          width: box.width,
          height: box.height,
          clientWidth: note.clientWidth,
          scrollWidth: note.scrollWidth,
        }
      })()`)
      assert.equal(measured.fontSize, "11.5px")
      assert.equal(measured.whiteSpace, "nowrap")
      assert.ok(measured.scrollWidth <= measured.clientWidth, "the whole note fits instead of clipping: " + JSON.stringify(measured))
      assert.ok(measured.height <= 16, "the note occupies one text line: " + JSON.stringify(measured))
      await tab.shot("reading-90-seconds")
    } finally {
      readingScenario = "normal"
    }
  }))

test("a reading beyond the retention window removes the row and says the source is missing", () =>
  inTab(async (tab) => {
    readingScenario = "expired"
    try {
      await tab.go("/")
      const seen = await tab.until("the expired row is gone and the batch says missing", (s) =>
        s.order.length === 0 && (s.readingBanner ?? "").includes("沒有仍可採用的上次讀數"),
      )
      assert.deepEqual(seen.order, [])
      assert.match(seen.readingBanner ?? "", /沒有仍可採用的上次讀數/)
      await tab.shot("reading-expired")
    } finally {
      readingScenario = "normal"
    }
  }))
