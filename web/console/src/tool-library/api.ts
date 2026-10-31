import { captureMachineFetch } from "../cloud/install.js"
import type { Library, Prepared, Project } from "./model.js"

export function libraryConnection() {
  const target = captureMachineFetch()
  async function request(path: string, signal: AbortSignal, body?: unknown, key?: string): Promise<unknown> {
    const response = await target.fetch(path, { signal, cache: "no-store", ...(body === undefined ? {} : {
      method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": key || crypto.randomUUID() }, body: JSON.stringify(body),
    }) })
    const answer = await response.json()
    if (!response.ok) throw Object.assign(new Error(typeof answer.detail === "string" ? answer.detail : "Tool library request failed"), { code: answer.error?.code || answer.error, status: response.status })
    if (!target.current()) throw Object.assign(new Error("The selected machine changed"), { code: "stale_session" })
    return answer
  }
  return {
    current: target.current,
    async read(signal: AbortSignal): Promise<Library> {
      const value = await request("/v1/tool-library", signal) as Library
      if (!Number.isSafeInteger(value?.version) || !Array.isArray(value.saved) || !Array.isArray(value.catalog?.projects) || !Array.isArray(value.catalog?.categories)) throw new Error("Invalid tool library reply")
      return value
    },
    write(body: { expected_version: number; project?: Project; remove?: string }, key: string, signal: AbortSignal): Promise<Pick<Library, "version" | "saved">> {
      return request("/v1/tool-library", signal, body, key) as Promise<Pick<Library, "version" | "saved">>
    },
    prepare(body: object, key: string, signal: AbortSignal): Promise<Prepared> {
      return request("/v1/tool-library/prepare", signal, body, key) as Promise<Prepared>
    },
  }
}
