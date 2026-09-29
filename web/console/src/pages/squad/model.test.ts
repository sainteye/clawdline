import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node's strip-types runner uses the source .ts extension.
import { ReceiptGate, visiblePersonas, type SquadReceipt, type SquadView } from "./model.ts"

const field = <T>(value: T) => ({ global: value, value, source: "default" as const, present: false, version: 1 })

test("team and search use data teams, including ones outside the old built-in list", () => {
  const data: SquadView = {
    scopeId: "global", project: null, projects: [], teams: [{ id: "community.design", name: "社群設計隊" }],
    personas: [{ id: "community.persona.a", settingsVersion: 1, name: "長名稱設計角色", subtitle: "Designer", summary: "看長文字", body: "", source: "", version: "1",
      icon: { accent: "#fff", cells: [["#fff"]] }, teamIds: ["community.design"], enabled: field(true), handbook: field(""), skillsSetting: field([]), skills: [] }],
    sessions: [], motion: field(true), motionSettingsVersion: 1, catalogVersion: 1, canWrite: true, partial: false,
  }
  assert.equal(visiblePersonas(data, "community.design", " 長名稱 ").length, 1)
  assert.equal(visiblePersonas(data, "engineering", "").length, 0)
  assert.equal(visiblePersonas(data, "", "no match").length, 0)
})

test("receipts play once only for a live applied event with the exact session, persona and skill version", () => {
  const gate = new ReceiptGate()
  const skill = { id: "skill.a", name: "Skill A", purpose: "", body: "", source: "", version: "v2", license: "", status: "available" as const, enabled: field(true), order: 1 }
  const receipt: SquadReceipt = { seq: 11, receiptId: "r11", snapshotId: "snap.a", conversationId: "session.a", definitionId: "persona.a", scopeId: "global", skillId: "skill.a", skillVersion: "v2", status: "applied", at: 1 }
  gate.baseline(10)
  const sessions = [{ sessionId: "terminal.a", conversationId: "session.a", definitionId: "persona.a", scopeId: "global", snapshotId: "snap.a", label: "Session A" }]
  assert.equal(gate.take({ ...receipt, seq: 10 }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, conversationId: "session.b" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 12, receiptId: "r12", scopeId: "project-b" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 13, receiptId: "r13", snapshotId: "other" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 14, receiptId: "r14", definitionId: "persona.b" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 15, receiptId: "r15", skillVersion: "v1" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 16, receiptId: "r16", status: "read" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 17, receiptId: "r17", status: "failed" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 18, receiptId: "r18" }, false, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 19, receiptId: "r19" }, true, sessions, "global", "persona.a", [skill]), true)
  assert.equal(gate.take({ ...receipt, seq: 20, receiptId: "r19" }, true, sessions, "global", "persona.a", [skill]), false)
  assert.equal(gate.take({ ...receipt, seq: 21, receiptId: "r21" }, true, sessions, "global", "persona.a", [skill]), true)
})
