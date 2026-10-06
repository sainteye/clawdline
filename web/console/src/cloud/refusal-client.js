// The copied client normalizes an authenticated machine reply into CloudFailure,
// discarding detail_key. Carry only that reply's signed key across its synchronous
// settle and event callbacks; never infer a key from the code or English words.
import { CloudClient } from "../legacy/js/net/cloud-client.js"

const KEY = /^http\.[0-9a-f]{16}$/u
const SIBLING = /^(read|action):(.+)$/u
const authenticated = new WeakMap()

/** Only an authenticated transcript refusal can contribute a Cloud detail key. */
export function authenticatedRefusalKey(error) {
  return error && typeof error === "object" ? authenticated.get(error)?.detailKey || null : null
}

export function withCatalogRefusals(BaseClient) {
  return class extends BaseClient {
    constructor(...args) { super(...args) }

    _applySnapshot(channel, payload, envelope, realign) {
      const refusal = channel?.kind === "transcript" && payload && typeof payload === "object"
        && typeof payload.read === "string" && payload.error && typeof payload.error === "object"
        && !Array.isArray(payload.error) ? payload.error : null
      const previous = this._catalogRefusal
      this._catalogRefusal = refusal && typeof refusal.code === "string"
        ? {
            read: payload.read,
            code: refusal.code,
            message: typeof refusal.message === "string" ? refusal.message : null,
            detailKey: typeof refusal.detail_key === "string" && KEY.test(refusal.detail_key) ? refusal.detail_key : null,
            envelope,
          }
        : null
      try { return super._applySnapshot(channel, payload, envelope, realign) }
      finally { this._catalogRefusal = previous }
    }

    _catalogDetail(error) {
      const context = this._catalogRefusal
      if (!context || !error || typeof error !== "object" || error.code !== context.code
        || (context.message !== null && error.message !== context.message)) return
      authenticated.set(error, context)
      // Display metadata must never make an otherwise valid reply fail.
      try {
        if (context.detailKey) error.detailKey = context.detailKey
        if (context.message !== null) error.wireDetail = context.message
      } catch { /* a frozen Error can still be carried by the WeakMap */ }
    }

    _settleRead(key, body, error) {
      const context = this._catalogRefusal
      const sibling = SIBLING.exec(context?.read || "")
      const siblingRead = sibling && (sibling[1] === "read" ? "action:" : "read:") + sibling[2]
      if (context && typeof key === "string" &&
        (key.endsWith("\u0000" + context.read) || (siblingRead && key.endsWith("\u0000" + siblingRead)))) {
        this._catalogDetail(error)
      }
      return super._settleRead(key, body, error)
    }

    _emit(event) {
      const context = this._catalogRefusal
      if (context && event?.type === "read" && event.read === context.read
        && event.envelope === context.envelope) this._catalogDetail(event.error)
      return super._emit(event)
    }
  }
}

export const CatalogCloudClient = withCatalogRefusals(CloudClient)
