import { client, mayWriteThroughCurrentTransport } from "../../client.js"
import type { SquadReceipt, SquadSession, SquadView } from "./model.js"
import { squadView, type WireCatalog, type WireSettings } from "./wire.js"
import type { Icon } from "@clawdline/contract"

export interface NewSquadSkill {
  skill_id: string
  version: string
  source: string
  license: string
  name: { en: string; "zh-Hant": string }
  purpose: { en: string; "zh-Hant": string }
  icon: Icon
  content: string
  folder?: boolean
  files?: { path: string; content_base64: string }[]
}

export interface PackPreview {
  digest: string
  previewDigest: string
  previewToken: string
  archiveBase64: string
  scopeId: string
  catalogVersion: number
  additions: string[]
  updates: string[]
  conflicts: { kind: string; id: string; reason: string }[]
  dependencies: string[]
  source: string
  license: string
  privateScopes: string[]
}

export interface SquadAPI {
  read(scopeId: string): Promise<SquadView>
  readCatalog(): Promise<WireCatalog>
  createSkill(skill: NewSquadSkill, expectedVersion: number, key: string): Promise<void>
  boundSessions(): Promise<SquadSession[]>
  saveHandbook(scopeId: string, personaId: string, value: string, expectedVersion: number): Promise<void>
  restoreHandbook(scopeId: string, personaId: string, expectedVersion: number): Promise<void>
  saveEnabled(scopeId: string, personaId: string, value: boolean, expectedVersion: number): Promise<void>
  restoreEnabled(scopeId: string, personaId: string, expectedVersion: number): Promise<void>
  saveMotion(scopeId: string, value: boolean, expectedVersion: number): Promise<void>
  restoreMotion(scopeId: string, expectedVersion: number): Promise<void>
  saveSkills(scopeId: string, personaId: string, choices: { id: string; version: string; enabled: boolean }[], expectedVersion: number): Promise<void>
  restoreSkills(scopeId: string, personaId: string, expectedVersion: number): Promise<void>
  preview(file: File, scopeId: string): Promise<PackPreview>
  adopt(preview: PackPreview, choices: Record<string, string>, privateScopes: string[]): Promise<void>
  exportPackage(includeGlobal: boolean, projectIds: string[]): Promise<{ blob: Blob; fileName: string }>
  eventHead(): Promise<number>
  events(after: number): Promise<{ events: SquadReceipt[]; nextAfter: number; hasMore: boolean }>
}

interface ScopeRow { place_id?: string; scope_id: string; kind: "repo" | "place"; label: string; path: string }
interface ScopesReply { scopes?: ScopeRow[]; scope_ids?: string[] }
interface SessionReply { sessions?: { sessionId?: string; label?: string; id?: string }[] }
interface BindingReply { bindings?: { session_id: string; conversation_id?: string; state: string; snapshot_id?: string; definition_id?: string; scope_id?: string }[] }

class SquadError extends Error {
  constructor(readonly code: string, detail: string, readonly status: number) { super(detail) }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), init.method ? 30_000 : 10_000)
  let response: Response
  try { response = await fetch(client.url(path), { ...init, signal: controller.signal }) }
  catch (error) {
    if (error instanceof Error && error.name === "AbortError") throw new SquadError("timeout", "角色小隊服務回應逾時。", 0)
    throw new SquadError("offline", "目前無法連線至角色小隊服務。", 0)
  }
  finally { clearTimeout(timer) }
  let value: unknown
  try { value = await response.json() }
  catch { throw new SquadError("invalid_response", "角色小隊服務回傳了無法讀取的資料。", response.status) }
  if (!response.ok) {
    const body = value as { error?: string | { code?: string; message?: string }; detail?: string }
    const code = typeof body?.error === "string" ? body.error : body?.error?.code ?? "request_failed"
    const detail = body?.detail || (typeof body?.error === "object" ? body.error.message : "") || "請求失敗（" + response.status + "）。"
    throw new SquadError(code, detail, response.status)
  }
  return value as T
}

function write(body: unknown, key: string = crypto.randomUUID()): RequestInit {
  return { method: "PUT", headers: { "Content-Type": "application/json", "Idempotency-Key": key }, body: JSON.stringify(body) }
}

function post(body: unknown, key?: string): RequestInit {
  return { ...write(body, key), method: "POST" }
}

function base64Of(bytes: Uint8Array): string {
  let binary = ""
  for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
  return btoa(binary)
}

function conflictReason(code?: string): string {
  switch (code) {
    case "reserved_id": return "內建項目不能覆蓋"
    case "name_conflict": return "名稱已由其他 ID 使用"
    case "version_content_conflict": return "版本相同但內容不同"
    case "version_downgrade": return "資料包版本比現有版本舊"
    case "version_incomparable": return "版本無法比較"
    default: return code || "目錄衝突"
  }
}

class HttpSquadAPI implements SquadAPI {
  private scopes = new Map<string, ScopeRow>()

  private async loadScopes(): Promise<ScopeRow[]> {
    const reply = await request<ScopesReply>("/v1/squad/scopes")
    this.scopes = new Map((reply.scopes ?? []).map((row) => [row.scope_id, row]))
    for (const id of reply.scope_ids ?? []) {
      if (!this.scopes.has(id)) this.scopes.set(id, { scope_id: id, kind: id.startsWith("place:") ? "place" : "repo", label: id, path: "" })
    }
    return [...this.scopes.values()]
  }

  private async scopeRef(scopeId: string): Promise<{ place_id?: string; scope_id?: string }> {
    if (!scopeId) return {}
    let row = this.scopes.get(scopeId)
    if (!row) { await this.loadScopes(); row = this.scopes.get(scopeId) }
    if (!row) throw new SquadError("unknown_project", "找不到這個 Project 範圍；請重新讀取清單。", 404)
    return row.place_id ? { place_id: row.place_id } : { scope_id: scopeId }
  }

  async read(scopeId: string): Promise<SquadView> {
    const [catalog, global, scopesResult, sessionsResult] = await Promise.all([
      request<WireCatalog>("/v1/squad/catalog"),
      request<WireSettings>("/v1/squad/settings"),
      this.loadScopes().then((value) => ({ ok: true as const, value }), () => ({ ok: false as const, value: [] as ScopeRow[] })),
      this.boundSessions().then((value) => ({ ok: true as const, value }), () => ({ ok: false as const, value: [] as SquadSession[] })),
    ])
    const ref = await this.scopeRef(scopeId)
    const current = scopeId ? await request<WireSettings>("/v1/squad/settings?" + new URLSearchParams(ref as Record<string, string>)) : global
    const projects = scopesResult.value.map((row) => ({ id: row.scope_id, kind: row.kind, name: row.label || row.scope_id, path: row.path }))
    const view = squadView(catalog, global, current, projects, sessionsResult.value, mayWriteThroughCurrentTransport())
    view.partial = !scopesResult.ok || !sessionsResult.ok
    return view
  }

  readCatalog(): Promise<WireCatalog> { return request<WireCatalog>("/v1/squad/catalog") }

  async createSkill(skill: NewSquadSkill, expectedVersion: number, key: string): Promise<void> {
    await request("/v1/squad/catalog", post({ kind: "skill", expected_version: expectedVersion, entity: { skill } }, key))
  }

  async boundSessions(): Promise<SquadSession[]> {
    const [sessionList, bindingList] = await Promise.all([
      request<SessionReply>("/v1/sessions"), request<BindingReply>("/v1/squad/session-bindings"),
    ])
    const byID = new Map((sessionList.sessions ?? []).map((row) => [row.id, row]))
    return (bindingList.bindings ?? []).flatMap((binding) => {
      const row = byID.get(binding.session_id)
      if (binding.state !== "bound" || !row || !row.sessionId || row.sessionId !== binding.conversation_id ||
        !binding.snapshot_id || !binding.definition_id || !binding.scope_id) return []
      return [{ sessionId: binding.session_id, conversationId: binding.conversation_id, snapshotId: binding.snapshot_id,
        definitionId: binding.definition_id, scopeId: binding.scope_id, label: row.label || binding.session_id }]
    })
  }

  private async personaWrite(scopeId: string, personaId: string, field: "handbook" | "auto_assign" | "skills", value: unknown, present: boolean, expectedVersion: number): Promise<void> {
    await request("/v1/squad/settings", write({ ...await this.scopeRef(scopeId), definition_id: personaId, expected_version: expectedVersion,
      overrides: { [field]: { present, value } } }))
  }
  saveHandbook(scopeId: string, personaId: string, value: string, expectedVersion: number) { return this.personaWrite(scopeId, personaId, "handbook", value, true, expectedVersion) }
  restoreHandbook(scopeId: string, personaId: string, expectedVersion: number) { return this.personaWrite(scopeId, personaId, "handbook", "", false, expectedVersion) }
  saveEnabled(scopeId: string, personaId: string, value: boolean, expectedVersion: number) { return this.personaWrite(scopeId, personaId, "auto_assign", value, true, expectedVersion) }
  restoreEnabled(scopeId: string, personaId: string, expectedVersion: number) { return this.personaWrite(scopeId, personaId, "auto_assign", false, false, expectedVersion) }
  saveSkills(scopeId: string, personaId: string, choices: { id: string; version: string; enabled: boolean }[], expectedVersion: number) { return this.personaWrite(scopeId, personaId, "skills", choices, true, expectedVersion) }
  restoreSkills(scopeId: string, personaId: string, expectedVersion: number) { return this.personaWrite(scopeId, personaId, "skills", [], false, expectedVersion) }
  private async motionWrite(scopeId: string, value: boolean, present: boolean, expectedVersion: number): Promise<void> {
    await request("/v1/squad/motion", write({ ...await this.scopeRef(scopeId), expected_version: expectedVersion, motion: { present, value } }))
  }
  saveMotion(scopeId: string, value: boolean, expectedVersion: number) { return this.motionWrite(scopeId, value, true, expectedVersion) }
  restoreMotion(scopeId: string, expectedVersion: number) { return this.motionWrite(scopeId, false, false, expectedVersion) }

  async preview(file: File, scopeId: string): Promise<PackPreview> {
    if (file.size > 512 * 1024) throw new SquadError("archive_too_large", "資料包超過 512 KiB 上限。", 413)
    const archiveBase64 = base64Of(new Uint8Array(await file.arrayBuffer()))
    const target = scopeId || "global"
    const reply = await request<{ archive_digest: string; preview_digest: string; preview_token: string; catalog_version: number;
      scope: string; source?: string; license?: string; private_scopes?: string[];
      changes: { kind: string; id: string; version: string; action: string; conflict_code?: string; dependents: string[] }[] }>(
      "/v1/squad-packages/preview", post({ archive_base64: archiveBase64, scope_id: target }))
    const label = (change: { kind: string; id: string; version: string }) => change.kind + " · " + change.id + " · " + change.version
    return {
      digest: reply.archive_digest, previewDigest: reply.preview_digest, previewToken: reply.preview_token,
      archiveBase64, scopeId: reply.scope, catalogVersion: reply.catalog_version,
      additions: reply.changes.filter((change) => change.action === "add").map(label),
      updates: reply.changes.filter((change) => change.action === "update").map(label),
      conflicts: reply.changes.filter((change) => change.action === "conflict").map((change) => ({ kind: change.kind, id: change.id, reason: conflictReason(change.conflict_code) })),
      dependencies: [...new Set(reply.changes.flatMap((change) => change.dependents))],
      source: reply.source || "", license: reply.license || "", privateScopes: reply.private_scopes ?? [],
    }
  }
  async adopt(preview: PackPreview, choices: Record<string, string>, privateScopes: string[]): Promise<void> {
    await request("/v1/squad-packages/adopt", post({ archive_base64: preview.archiveBase64,
      archive_digest: preview.digest, preview_digest: preview.previewDigest, preview_token: preview.previewToken,
      catalog_version: preview.catalogVersion, scope_id: preview.scopeId, choices,
      private_scopes: privateScopes, confirm_private: privateScopes.length > 0 }))
  }
  async exportPackage(includeGlobal: boolean, projectIds: string[]): Promise<{ blob: Blob; fileName: string }> {
    const privateScopes = [...(includeGlobal ? ["global"] : []), ...projectIds]
    const reply = await request<{ archive_base64: string; file_name: string; mime_type: string }>(
      "/v1/squad-packages/export", post({ private_scopes: privateScopes, confirm_private: privateScopes.length > 0 }))
    const bytes = Uint8Array.from(atob(reply.archive_base64), (letter) => letter.charCodeAt(0))
    return { blob: new Blob([bytes], { type: reply.mime_type || "application/zip" }), fileName: reply.file_name || "clawdline-squad.zip" }
  }

  async eventHead(): Promise<number> {
    const reply = await request<{ seq: number }>("/v1/squad/events/head")
    return reply.seq
  }
  async events(after: number): Promise<{ events: SquadReceipt[]; nextAfter: number; hasMore: boolean }> {
    const reply = await request<{ events: { seq: number; receipt_id: string; snapshot_id: string; conversation_id: string; definition_id: string; scope_id: string; skill_id: string; skill_version: string; status: SquadReceipt["status"]; at: number }[]; next_after: number; has_more: boolean }>("/v1/squad/events?after=" + after + "&limit=50")
    return { events: reply.events.map((entry) => ({ seq: entry.seq, receiptId: entry.receipt_id, snapshotId: entry.snapshot_id, conversationId: entry.conversation_id,
      definitionId: entry.definition_id, scopeId: entry.scope_id, skillId: entry.skill_id, skillVersion: entry.skill_version,
      status: entry.status, at: entry.at })), nextAfter: reply.next_after, hasMore: reply.has_more }
  }
}

export const squadApi: SquadAPI = new HttpSquadAPI()
