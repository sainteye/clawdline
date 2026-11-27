import type * as W from "@clawdline/contract"
import { test } from "node:test"
import assert from "node:assert/strict"
import {
  BotRequestRefused, actorName, checklistProgress, handoffHistory, lastUpdated, linkRequestID, noteParts, placeBotRequests,
  progressLabel, progressOf, readBotRequests, readBotWorkShown, readsBotRequests, refusalWords, sortBotRequests, writeBotRequest, writeBotWorkShown,
  // @ts-expect-error Node executes source extensions.
} from "./bot-request.ts"
// @ts-expect-error Node executes source extensions.
import { words } from "./words.ts"

const w = words("zh-Hant"), en = words("en")
const card = (extra: Partial<W.BotRequestView> = {}): W.BotRequestView => ({
  id: "c1", title: "Publish the report", project_id: "p", project_path: "/p", created_by: "atlas", created_at: "2026-11-24T08:00:00Z",
  updated_at: "2026-11-24T09:00:00Z", authority: [], baton: {bot: "atlas", role: "maker"}, chain: ["atlas"], checklist: [], cycle: 1,
  done: 0, total: 0, progress_state: "in_progress", state: "open", version: 1, ...extra,
})
const bot = (id: string, name: string) => ({key: {machine_id: "m", bot_id: id}, name}) as W.ProjectAssistantBot
const link = (id: string, request?: string) => ({id, task_id: "t-" + id, title: "Work " + id, state: "executing", bot_request_id: request}) as unknown as W.ProjectAssistantWorkLink
const json = (status: number, body: unknown) => async () => new Response(typeof body === "string" ? body : JSON.stringify(body), {status})

test("a card's light is the daemon's progress state, in the words the person chose", () => {
  assert.equal(progressLabel(progressOf(card()), w), "進行中")
  assert.equal(progressLabel(progressOf(card({progress_state: "waiting_for_you", waiting: "q1"})), w), "等你")
  assert.equal(progressLabel(progressOf(card({progress_state: "done", state: "done"})), w), "已完成")
  assert.equal(progressLabel(progressOf(card({progress_state: "cancelled", state: "cancelled"})), w), "已取消")
  assert.equal(progressLabel(progressOf(card({progress_state: "merged", state: "merged"})), en), "Merged into another card")
})

test("a progress word this console does not know falls back to the card's own state", () => {
  assert.equal(progressOf(card({progress_state: "paused", state: "done"})), "done")
  assert.equal(progressOf(card({progress_state: "paused", waiting: "q1"})), "waiting")
  assert.equal(progressOf(card({progress_state: "paused"})), "in_progress")
})

test("checklist progress reads n/m ✓ and says nothing for a card without a checklist", () => {
  assert.equal(checklistProgress(card({done: 2, total: 5}), w), "2/5 ✓")
  assert.equal(checklistProgress(card({done: 0, total: 3}), w), "0/3 ✓")
  assert.equal(checklistProgress(card({done: 0, total: 0}), w), "")
})

test("cards waiting for the person sort first, then in progress, then closed, newest update first", () => {
  const sorted = sortBotRequests([
    card({id: "done", progress_state: "done", state: "done", updated_at: "2026-11-24T12:00:00Z"}),
    card({id: "old", updated_at: "2026-11-24T07:00:00Z"}),
    card({id: "waiting", progress_state: "waiting_for_you", updated_at: "2026-11-24T06:00:00Z"}),
    card({id: "new", updated_at: "2026-11-24T10:00:00Z"}),
  ])
  assert.deepEqual(sorted.map((row: W.BotRequestView) => row.id), ["waiting", "new", "old", "done"])
})

test("last updated counts minutes, hours and days, and an unreadable time says nothing", () => {
  const now = Date.parse("2026-11-24T12:00:00Z")
  assert.equal(lastUpdated("2026-11-24T11:59:40Z", now, w), "剛剛更新")
  assert.equal(lastUpdated("2026-11-24T11:55:00Z", now, w), "最後更新 5 分鐘前")
  assert.equal(lastUpdated("2026-11-24T09:00:00Z", now, w), "最後更新 3 小時前")
  assert.equal(lastUpdated("2026-11-21T12:00:00Z", now, en), "Updated 3 d ago")
  assert.equal(lastUpdated("2026-11-24T13:00:00Z", now, w), "剛剛更新")
  assert.equal(lastUpdated("", now, w), "")
})

test("a handoff names its receiver only when the chain settles it", () => {
  const events = [
    {id: 1, kind: "card.created", actor: "bot:atlas", at: "2026-11-24T08:00:00Z", version: 1},
    {id: 2, kind: "baton.handed", actor: "bot:atlas", at: "2026-11-24T08:10:00Z", version: 2, detail: "Please review"},
    {id: 3, kind: "baton.handed", actor: "person", at: "2026-11-24T08:20:00Z", version: 3},
  ]
  assert.deepEqual(handoffHistory(card({events, chain: ["atlas", "nova", "orion"]})), [
    {id: 2, from: "bot:atlas", to: "nova", at: "2026-11-24T08:10:00Z", note: "Please review"},
    {id: 3, from: "person", to: "orion", at: "2026-11-24T08:20:00Z", note: ""},
  ])
  // Handed back to a Bot already in the chain: the history does not say to whom.
  assert.deepEqual(handoffHistory(card({events, chain: ["atlas", "nova"]})).map((row: {to?: string}) => row.to), [undefined, undefined])
  const joined = [...events, {id: 4, kind: "chain.joined", actor: "bot:atlas", at: "2026-11-24T08:30:00Z", version: 4, detail: "orion"}]
  assert.deepEqual(handoffHistory(card({events: joined, chain: ["atlas", "nova", "orion"]})).map((row: {to?: string}) => row.to), [undefined, undefined])
  assert.deepEqual(handoffHistory(card()), [])
})

test("an actor is the person, a Bot by its real name, or a Bot this device cannot read", () => {
  const bots = [bot("atlas", "Atlas")]
  assert.equal(actorName("person", bots, w), "你")
  assert.equal(actorName("bot:atlas", bots, w), "Atlas")
  assert.equal(actorName("atlas", bots, w), "Atlas")
  assert.equal(actorName("bot:gone", bots, w), "這台裝置讀不到的 Bot")
  assert.equal(actorName(undefined, bots, w), "這台裝置讀不到的 Bot")
})

test("only http and https addresses in a note become links", () => {
  assert.deepEqual(noteParts("Live at https://example.com/a?b=1. Commit abc123"), [
    {text: "Live at "}, {text: "https://example.com/a?b=1", href: "https://example.com/a?b=1"}, {text: ". Commit abc123"},
  ])
  assert.deepEqual(noteParts("javascript:alert(1) file:///etc"), [{text: "javascript:alert(1) file:///etc"}])
  assert.deepEqual(noteParts(""), [])
})

test("a card sits above the first work result of its own, and a card without work stays loose while open", () => {
  const cards = [card({id: "a"}), card({id: "b"}), card({id: "closed", state: "done", progress_state: "done"})]
  const placed = placeBotRequests(cards, [link("1", "a"), link("2"), link("3", "a"), link("4", "unknown"), link("1", "a")])
  assert.deepEqual([...placed.above].map(([id, value]) => [id, value.id]), [["1", "a"]])
  assert.deepEqual(placed.links.get("a")!.map((row: W.ProjectAssistantWorkLink) => row.id), ["1", "3"])
  assert.deepEqual(placed.loose.map((row: W.BotRequestView) => row.id), ["b"])
  assert.equal(linkRequestID(link("2")), "")
})

test("a chat without cards places nothing", () => {
  const placed = placeBotRequests([], [link("1", "a"), link("2")])
  assert.equal(placed.above.size, 0)
  assert.equal(placed.links.size, 0)
  assert.deepEqual(placed.loose, [])
})

test("a refusal is said in plain words, and an unknown code still says the change failed", () => {
  assert.equal(refusalWords("bot_request_version_conflict", w), "這張卡剛剛有變動，已經重新載入。請確認後再試一次。")
  assert.equal(refusalWords("bot_request_closed", w), "這張卡已經結束。")
  assert.equal(refusalWords("bot_request_forbidden", en), "This device isn’t allowed to change this card.")
  assert.equal(refusalWords("something_new", w), "無法更改這張卡，請再試一次。")
  assert.equal(refusalWords("unconfirmed", w), "無法確認這次更改有沒有存下來。這張卡已重新載入，請確認後再試。")
})

test("no card route hides the feature; a failed read is an error, never an empty list", async () => {
  assert.equal(await readBotRequests({}, undefined, json(404, {error: "not_found"})), null)
  assert.equal(await readBotRequests({}, undefined, json(200, "<!doctype html><title>console</title>")), null)
  assert.deepEqual(await readBotRequests({}, undefined, json(200, {cards: []})), [])
  await assert.rejects(readBotRequests({}, undefined, json(500, {error: "store_unavailable", detail: ""})),
    (error: unknown) => error instanceof BotRequestRefused && error.code === "store_unavailable")
  await assert.rejects(readBotRequests({}, undefined, async () => { throw new TypeError("offline") }))
  let asked = ""
  await readBotRequests({bot: "atlas", state: "open"}, undefined, async (path: string) => { asked = path; return new Response('{"cards":[]}') })
  assert.equal(asked, "/v1/bot-request/cards?bot=atlas&state=open")
})

test("every page of cards is read; a cursor that never ends is an error, not a short list", async () => {
  const asked: string[] = []
  const pages: Record<string, unknown> = {"": {cards: [card({id: "a"})], next_cursor: "p2"}, p2: {cards: [card({id: "b"})], next_cursor: "p3"}, p3: {cards: [card({id: "c"})]}}
  const paged = async (path: string) => {
    asked.push(path)
    const cursor = new URL(path, "http://x").searchParams.get("cursor") ?? ""
    return new Response(JSON.stringify(pages[cursor]))
  }
  assert.deepEqual((await readBotRequests({bot: "atlas"}, undefined, paged))?.map(c => c.id), ["a", "b", "c"])
  assert.deepEqual(asked, ["/v1/bot-request/cards?bot=atlas", "/v1/bot-request/cards?bot=atlas&cursor=p2", "/v1/bot-request/cards?bot=atlas&cursor=p3"])
  await assert.rejects(readBotRequests({}, undefined, async () => new Response(JSON.stringify({cards: [card({id: "a"})], next_cursor: "same"}))))
})

test("a write carries the version and its key, and a refusal keeps its code and current version", async () => {
  let sent: {path: string; init?: RequestInit} | undefined
  const result = await writeBotRequest("c 1", "complete", {expected_version: 4, reason: "Shipped"}, "key-1", undefined,
    async (path: string, init?: RequestInit) => { sent = {path, init}; return new Response(JSON.stringify({card: card({version: 5}), changed: true})) })
  assert.equal(result.card.version, 5)
  assert.equal(sent!.path, "/v1/bot-request/cards/c%201/complete")
  assert.equal((sent!.init!.headers as Record<string, string>)["Idempotency-Key"], "key-1")
  assert.deepEqual(JSON.parse(String(sent!.init!.body)), {expected_version: 4, reason: "Shipped"})
  await assert.rejects(writeBotRequest("c1", "cancel", {expected_version: 4}, "key-2", undefined,
    json(409, {error: "bot_request_version_conflict", detail: "expected_version", version: 6})),
    (error: unknown) => error instanceof BotRequestRefused && error.code === "bot_request_version_conflict" && error.version === 6)
  await assert.rejects(writeBotRequest("c1", "cancel", {expected_version: 4}, "key-3", undefined, async () => { throw new TypeError("offline") }),
    (error: unknown) => error instanceof BotRequestRefused && error.code === "unconfirmed")
})

test("cards are read only from the local daemon, and the Board section is shown until this browser hides it", () => {
  assert.equal(readsBotRequests({authorizationRevision: "local"}), true)
  assert.equal(readsBotRequests({authorizationRevision: "cloud-7"}), false)
  assert.equal(readsBotRequests(undefined), false)
  const kept = new Map<string, string>()
  const store = {getItem: (key: string) => kept.get(key) ?? null, setItem: (key: string, value: string) => { kept.set(key, value) }}
  assert.equal(readBotWorkShown(store), true)
  writeBotWorkShown(false, store)
  assert.equal(readBotWorkShown(store), false)
  writeBotWorkShown(true, store)
  assert.equal(readBotWorkShown(store), true)
  assert.equal(readBotWorkShown({getItem() { throw new Error("blocked") }, setItem() {}}), true)
  assert.equal(readBotWorkShown(null), true)
})
