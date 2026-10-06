import { test } from "node:test"
import assert from "node:assert/strict"
import type { UpdateApply, UpdateState, UpdateStatus } from "@clawdline/contract"
import {
  asksForUpdatePanel,
  classifyApplyAnswer,
  detailsText,
  INSTALL_COMMAND,
  classifyUpdateRead,
  settleUpdateRead,
  shortStamp,
  shouldLookForNewConsole,
  updateBannerVersion,
  updateNotice,
  updatePanel,
  UPDATE_FOLLOW_EVERY_MS,
  UPDATE_READ_EVERY_MS,
  type UpdatePanelInput,
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
} from "./update-model.ts"
// @ts-expect-error -- a `.ts` path for node's type-stripping test runner.
import { nextWord } from "../next-strings.ts"

function status(state: UpdateState): UpdateStatus {
  return {
    state,
    running: { stamp: "1a2b3c4d5e6f7a8b9c0d", committed_at: "2026-10-01T00:00:00Z" },
    latest: { stamp: "4b7c3f8d9e0f1a2b3c4d", committed_at: "2026-10-04T02:31:05Z" },
    checked_at: "2026-10-04T12:40:00Z",
    source_url: "https://example.invalid/BUILD.json",
  }
}

test("only a machine that trails or differs says anything", () => {
  for (const state of ["update_available", "differs"] as const) {
    const line = updateNotice(status(state), nextWord)
    assert.ok(line, state + " said nothing")
    assert.ok(line!.includes("1a2b3c4d") && !line!.includes("1a2b3c4d5"), line!)
    assert.ok(line!.includes("4b7c3f8d") && !line!.includes("4b7c3f8d9"), line!)
    assert.ok(line!.includes("clawdline update --apply"), line!)
  }
  for (const state of ["current", "ahead", "unknown"] as const) {
    assert.equal(updateNotice(status(state), nextWord), null, state + " spoke")
  }
})

test("a refused or failed read is silent, a daemon predating the route above all", () => {
  // 404 from a daemon that has no /v1/update.
  assert.equal(settleUpdateRead(false, { error: "not_found", detail: "no route" }), null)
  // The relay's answer for a daemon that predates the `update` word.
  assert.equal(settleUpdateRead(false, { error: { code: "unknown_command" } }), null)
  assert.equal(settleUpdateRead(false, null), null)
  assert.equal(settleUpdateRead(true, null), null)
  assert.equal(settleUpdateRead(true, { state: "update_available" }), null)
  assert.equal(updateNotice(null, nextWord), null)
  const read = settleUpdateRead(true, status("update_available"))
  assert.equal(read?.state, "update_available")
  assert.ok(updateNotice(read, nextWord))
})

test("the notice reads no more often than every ten minutes, and prints eight characters", () => {
  assert.ok(UPDATE_READ_EVERY_MS >= 10 * 60 * 1000)
  assert.equal(shortStamp("4b7c3f8d9e0f"), "4b7c3f8d")
  assert.equal(shortStamp(undefined), "")
})

// A release install, as the updater's GET /v1/update answers it.
function release(state: UpdateState, apply?: UpdateApply, extra: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    state,
    running: { stamp: "1a2b3c4d5e6f7a8b9c0d", version: "v0.10.0" },
    latest: { stamp: "4b7c3f8d9e0f1a2b3c4d", version: "v0.11.0", notes_url: "https://example.invalid/notes/v0.11.0" },
    checked_at: "2026-10-04T12:40:00Z",
    source_url: "https://example.invalid/manifest.json",
    install_kind: "release",
    channel: "stable",
    auto_apply: false,
    apply: apply ?? { state: "idle" },
    ...extra,
  }
}

function input(over: Partial<UpdatePanelInput> = {}): UpdatePanelInput {
  return {
    read: null, last: null, following: false, sending: false, pressRefused: null, pressOlder: false,
    overCloud: false, autoSaving: false, autoFailed: "", ...over,
  }
}

const iso = (s: string) => "at " + s
const panel = (over: Partial<UpdatePanelInput>) => updatePanel(input(over), nextWord, iso)
const answered = (s: UpdateStatus) => ({ kind: "status" as const, status: s })

test("a release install shows its version, the latest, the notes, the check and the channel", () => {
  const view = panel({ read: answered(release("update_available")) })
  assert.equal(view.kind, "panel")
  const facts = Object.fromEntries(view.facts.map((f) => [f.key, f]))
  assert.equal(facts.running.value, "v0.10.0")
  assert.equal(facts.latest.value, "v0.11.0")
  assert.equal(facts.latest.href, "https://example.invalid/notes/v0.11.0")
  assert.equal(facts.checked.value, "at 2026-10-04T12:40:00Z")
  assert.ok(facts.channel.value)
  assert.deepEqual([view.press.shown, view.press.enabled], [true, true])
  assert.equal(view.auto.shown, true)
  assert.equal(view.auto.enabled, true)
  assert.equal(view.everyMs, UPDATE_READ_EVERY_MS)
})

test("a current release offers no button and says it is current", () => {
  const view = panel({ read: answered(release("current")) })
  assert.equal(view.press.shown, false)
  assert.ok(view.stateLine)
})

test("each moving step is said, the button waits, and the panel reads every two seconds", () => {
  for (const step of ["downloading", "verifying", "staged", "restarting"] as const) {
    const view = panel({ read: answered(release("update_available", { state: step, from: "v0.10.0", to: "v0.11.0" })) })
    assert.ok(view.progress, step)
    assert.equal(view.press.enabled, false, step)
    assert.equal(view.everyMs, UPDATE_FOLLOW_EVERY_MS, step)
    assert.equal(view.problem, null, step)
  }
})

test("the restart's silence reads as restarting, never as a failure", () => {
  const moving = release("update_available", { state: "restarting", from: "v0.10.0", to: "v0.11.0" })
  // The page saw the update move, then nothing answered.
  let view = panel({ read: { kind: "unreachable" }, last: moving })
  assert.equal(view.restarting, true)
  assert.equal(view.progress, nextWord("updateRestarting"))
  assert.equal(view.problem, null)
  assert.equal(view.everyMs, UPDATE_FOLLOW_EVERY_MS)
  // The page pressed the button and never saw a status before the silence.
  view = panel({ read: { kind: "unreachable" }, last: null, following: true })
  assert.equal(view.kind, "panel")
  assert.equal(view.progress, nextWord("updateRestarting"))
  // Silence with nothing being followed is just silence.
  view = panel({ read: { kind: "unreachable" }, last: release("current") })
  assert.equal(view.restarting, false)
  assert.equal(view.progress, null)
  assert.equal(view.problem, null)
})

test("a rolled-back or failed update says plainly that the previous version runs, and folds the code away", () => {
  const rolled = panel({ read: answered(release("update_available", {
    state: "rolled_back", from: "v0.10.0", to: "v0.11.0", error: { code: "health_timeout", detail: "no answer in 60 s" },
  })) })
  const sentence = rolled.problem!.sentence
  assert.equal(rolled.problem!.kind, "rolled_back")
  assert.ok(sentence.includes("v0.10.0") && sentence.includes("v0.11.0"), sentence)
  assert.ok(!sentence.includes("health_timeout") && !sentence.includes("no answer"), "the code is not in the sentence: " + sentence)
  assert.ok(sentence.includes("Automatic updates will not try this version again"), sentence)
  assert.deepEqual(rolled.problem!.details, { code: "health_timeout", detail: "no answer in 60 s" })
  assert.equal(detailsText(rolled.problem!.details!), "health_timeout — no answer in 60 s")
  assert.equal(rolled.press.enabled, true, "a person may try again")
  assert.equal(rolled.press.label, nextWord("updateRetry"))
  assert.equal(rolled.press.below, true, "the button sits under the explanation")
  const failed = panel({ read: answered(release("update_available", {
    state: "failed", from: "v0.10.0", to: "v0.11.0", error: { code: "download_failed" },
  })) })
  assert.equal(failed.problem!.kind, "failed")
  assert.ok(failed.problem!.sentence.includes("v0.10.0") && !failed.problem!.sentence.includes("download_failed"), failed.problem!.sentence)
  assert.ok(!failed.problem!.sentence.includes("Automatic updates"), "a failure before switching is not remembered as failed: " + failed.problem!.sentence)
  assert.deepEqual(failed.problem!.details, { code: "download_failed", detail: "" })
  assert.equal(failed.press.label, nextWord("updateRetry"))
})

test("after a rollback, a newer release than the one that failed is offered as an update, not a retry", () => {
  const view = panel({ read: answered(release("update_available", { state: "rolled_back", from: "v0.10.0", to: "v0.10.5" })) })
  assert.equal(view.press.label, nextWord("updateNow"))
})

test("a finished update stays said with when and from what, after a reload and after an overnight auto-update", () => {
  const healthy = release("current", { state: "healthy", from: "v0.10.0", to: "v0.11.0", at: "2026-10-07T03:12:00Z" }, {
    running: { stamp: "4b7c3f8d9e0f1a2b3c4d", version: "v0.11.0" },
  })
  // Not following: the page reloaded into the new console, or nobody watched.
  const view = panel({ read: answered(healthy) })
  assert.equal(view.done, nextWord("updateDoneAt", { when: "at 2026-10-07T03:12:00Z", from: "v0.10.0", to: "v0.11.0" }))
  assert.equal(view.progress, null)
  assert.equal(view.problem, null)
  const noTime = panel({ read: answered({ ...healthy, apply: { state: "healthy", from: "v0.10.0", to: "v0.11.0" } }) })
  assert.equal(noTime.done, nextWord("updateDone", { from: "v0.10.0", to: "v0.11.0" }))
  // A healthy record that does not say what it replaced says nothing of it.
  assert.equal(panel({ read: answered({ ...healthy, apply: { state: "healthy", to: "v0.11.0" } }) }).done, null)
  // A moving update is not a finished one.
  assert.equal(panel({ read: answered(release("update_available", { state: "downloading", from: "v0.10.0", to: "v0.11.0" })) }).done, null)
})

test("a newer release available says what pressing does", () => {
  const view = panel({ read: answered(release("update_available")) })
  assert.equal(view.stateLine, nextWord("updateIsAvailable", { latest: "v0.11.0" }))
  assert.ok(view.stateLine!.includes("tmux"), view.stateLine!)
})

test("a failed check and an unknown comparison say it plainly and fold the error away", () => {
  const failed = panel({ read: answered(release("update_available", undefined, { error: "Get \"https://x/manifest.json\": dial tcp: no such host" })) })
  assert.ok(failed.stateLine!.includes(nextWord("updateCheckFailed")), failed.stateLine!)
  assert.ok(!failed.stateLine!.includes("dial tcp"), failed.stateLine!)
  assert.equal(failed.stateDetails!.detail, "Get \"https://x/manifest.json\": dial tcp: no such host")
  const unknown = panel({ read: answered(release("unknown", undefined, { reason: "manifest signature did not verify" })) })
  assert.equal(unknown.stateLine, nextWord("updateIsUnknown"))
  assert.equal(unknown.stateDetails!.detail, "manifest signature did not verify")
  assert.equal(panel({ read: answered(release("current")) }).stateDetails, null)
})

test("the auto-update switch says 開 or 關 beside a name that never changes, and warns the beta channel", () => {
  const off = panel({ read: answered(release("current")) })
  assert.equal(off.auto.state, nextWord("updateAutoOff"))
  assert.equal(off.auto.beta, null)
  const on = panel({ read: answered(release("current", undefined, { auto_apply: true, channel: "beta" })) })
  assert.equal(on.auto.on, true)
  assert.equal(on.auto.state, nextWord("updateAutoOn"))
  assert.equal(on.auto.beta, nextWord("updateAutoBeta"))
})

test("only an address that asks for the update panel lands on it", () => {
  assert.equal(asksForUpdatePanel("#page=settings&focus=update"), true)
  assert.equal(asksForUpdatePanel("#focus=update&page=settings"), true)
  assert.equal(asksForUpdatePanel("#page=settings"), false)
  assert.equal(asksForUpdatePanel("#page=sessions&focus=update"), false)
  assert.equal(asksForUpdatePanel(""), false)
})

test("a healthy update with a staged app says it is replaced when the app quits", () => {
  const view = panel({
    read: answered(release("current", { state: "healthy", from: "v0.10.0", to: "v0.11.0", staged_app: "/x/Clawdline.app" }, {
      running: { stamp: "4b7c3f8d9e0f1a2b3c4d", version: "v0.11.0" },
    })),
    following: true,
  })
  assert.equal(view.staged, nextWord("updateStagedApp"))
  assert.equal(view.done, nextWord("updateDone", { from: "v0.10.0", to: "v0.11.0" }))
  assert.equal(view.progress, null, "said once, as what was done")
  assert.equal(view.press.shown, false)
})

test("a page that followed an update to healthy offers the next release that arrives", () => {
  // The same page, not reloaded: the console the new daemon served was the
  // one it already ran. A newer release is then one more press.
  const next = panel({
    following: true,
    read: answered(release("update_available", { state: "healthy", from: "v0.10.0", to: "v0.11.0" }, {
      running: { stamp: "4b7c3f8d9e0f1a2b3c4d", version: "v0.11.0" },
      latest: { stamp: "5c8d4e9f0a1b2c3d4e5f", version: "v0.12.0" },
    })),
  })
  assert.deepEqual([next.press.shown, next.press.enabled], [true, true])
  // What it just installed, still read as available, offers nothing yet.
  const same = panel({
    following: true,
    read: answered(release("update_available", { state: "healthy", from: "v0.10.0", to: "v0.11.0" })),
  })
  assert.equal(same.press.shown, false)
})

test("a refused press says nothing changed, and folds the code away", () => {
  const refused = panel({ read: answered(release("update_available")), pressRefused: { code: "not_installed_as_service", detail: "service.json is missing" } })
  assert.equal(refused.problem!.kind, "refused")
  assert.equal(refused.problem!.sentence, nextWord("updateRefused"))
  assert.deepEqual(refused.problem!.details, { code: "not_installed_as_service", detail: "service.json is missing" })
  assert.equal(refused.press.below, true)
})

test("a machine without the apply route keeps the panel and says to run the install command", () => {
  const view = panel({ read: answered(release("update_available")), pressOlder: true })
  assert.equal(view.kind, "panel")
  assert.equal(view.facts[0].value, "v0.10.0", "the versions stay")
  assert.equal(view.problem!.kind, "older")
  assert.equal(view.problem!.sentence, nextWord("updateApplyOlder"))
  assert.equal(view.problem!.command, INSTALL_COMMAND)
  assert.ok(INSTALL_COMMAND.startsWith("curl -fsSL https://") && INSTALL_COMMAND.endsWith("| sh"))
  assert.equal(view.press.shown, false, "pressing again cannot work")
  // With no status at all there is no panel to keep: the needs-update line.
  assert.equal(panel({ pressOlder: true }).kind, "older")
})

test("a source build has no button and one sentence on how it updates", () => {
  for (const kind of ["source_deploy", "source_checkout", "none"] as const) {
    const view = panel({ read: answered(release("update_available", undefined, { install_kind: kind, running: { stamp: "1a2b3c4d5e6f" }, latest: { stamp: "4b7c3f8d9e0f" } })) })
    assert.equal(view.press.shown, false, kind)
    assert.equal(view.auto.shown, false, kind)
    assert.ok(view.sourceNote, kind)
    assert.equal(view.facts[0].value, "1a2b3c4d", kind)
  }
  const deploy = panel({ read: answered(release("current", undefined, { install_kind: "source_deploy" })) })
  assert.ok(deploy.sourceNote!.includes("tools/deploy-linux-user.sh") && deploy.sourceNote!.includes("clawdline setup --adopt"))
})

test("an older daemon gets today's notice, never 讀取失敗", () => {
  const old = { ...release("update_available"), install_kind: undefined, apply: undefined, channel: undefined, auto_apply: undefined }
  const view = panel({ read: answered(old) })
  assert.equal(view.kind, "legacy")
  assert.equal(view.legacyLine, updateNotice(old, nextWord))
  // A daemon with no /v1/update at all says nothing.
  assert.equal(panel({ read: { kind: "older" } }).kind, "nothing")
  assert.equal(panel({ read: { kind: "refused" } }).kind, "nothing")
})

test("over Cloud the auto-update switch is shown and says where it is changed", () => {
  const view = panel({ read: answered(release("update_available", undefined, { auto_apply: true })), overCloud: true })
  assert.equal(view.auto.on, true)
  assert.equal(view.auto.enabled, false)
  assert.equal(view.auto.note, nextWord("updateAutoCloud"))
  assert.equal(view.press.enabled, true, "the press is carried over Cloud")
  const failed = panel({ read: answered(release("update_available")), autoFailed: "Only this machine's own token may change its settings." })
  assert.equal(failed.auto.note, "Only this machine's own token may change its settings.")
})

test("reads are sorted into older, unreachable and refused", () => {
  assert.equal(classifyUpdateRead({ transport: "failed" }).kind, "unreachable")
  assert.equal(classifyUpdateRead({ transport: "answered", status: 502, parsed: null }).kind, "unreachable")
  assert.equal(classifyUpdateRead({ transport: "answered", status: 404, parsed: { error: "not_found" } }).kind, "older")
  assert.equal(classifyUpdateRead({ transport: "answered", status: 501, parsed: { error: { code: "unknown_command" } } }).kind, "older")
  assert.equal(classifyUpdateRead({ transport: "answered", status: 403, parsed: { error: "forbidden" } }).kind, "refused")
  assert.equal(classifyUpdateRead({ transport: "answered", status: 200, parsed: release("current") }).kind, "status")
})

test("a press is sorted into started, older, refused and unreachable", () => {
  const started = classifyApplyAnswer({ transport: "answered", status: 202, parsed: release("update_available", { state: "downloading" }) })
  assert.equal(started.kind, "started")
  assert.equal(started.kind === "started" && started.status?.apply?.state, "downloading")
  assert.equal(classifyApplyAnswer({ transport: "answered", status: 501, parsed: { error: "not_implemented", detail: "x" } }).kind, "older")
  assert.equal(classifyApplyAnswer({ transport: "answered", status: 404, parsed: null }).kind, "older")
  const refused = classifyApplyAnswer({ transport: "answered", status: 409, parsed: { error: "update_in_progress", detail: "another update holds the lock" } })
  assert.deepEqual(refused, { kind: "refused", code: "update_in_progress", detail: "another update holds the lock" })
  const unreachableHost = classifyApplyAnswer({ transport: "answered", status: 502, parsed: { error: "release_unreachable", detail: "dns" } })
  assert.equal(unreachableHost.kind, "refused", "the release host's 502 is the machine's answer, not silence")
  assert.equal(classifyApplyAnswer({ transport: "failed" }).kind, "unreachable")
})

test("the page looks for a new console only after following an update to healthy, and never over Cloud", () => {
  const healthy = answered(release("current", { state: "healthy", from: "v0.10.0", to: "v0.11.0" }))
  assert.equal(shouldLookForNewConsole({ following: true, overCloud: false, read: healthy }), true)
  assert.equal(shouldLookForNewConsole({ following: false, overCloud: false, read: healthy }), false)
  assert.equal(shouldLookForNewConsole({ following: true, overCloud: true, read: healthy }), false)
  assert.equal(shouldLookForNewConsole({ following: true, overCloud: false, read: { kind: "unreachable" } }), false)
  assert.equal(shouldLookForNewConsole({ following: true, overCloud: false, read: answered(release("update_available", { state: "restarting" })) }), false)
})

test("the banner names a newer release until that version is dismissed", () => {
  assert.equal(updateBannerVersion(release("update_available"), null), "v0.11.0")
  assert.equal(updateBannerVersion(release("update_available"), "v0.11.0"), null)
  assert.equal(updateBannerVersion(release("update_available"), "v0.10.5"), "v0.11.0", "a dismissal is per version")
  assert.equal(updateBannerVersion(release("current"), null), null)
  assert.equal(updateBannerVersion(release("update_available", { state: "downloading" }), null), null)
  assert.equal(updateBannerVersion(release("update_available", undefined, { install_kind: "source_deploy" }), null), null)
  assert.equal(updateBannerVersion(null, null), null)
})
