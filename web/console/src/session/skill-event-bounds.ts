/*
 * How many conversations one filtered `/v1/squad/events` read may name, the
 * daemon's `store.MaxSquadEventConversations` (`squad.event_conversations` in
 * internal/domain/capacity, which checks this copy), and how long one ID may
 * be (`store.MaxSquadEventIDBytes`). The Session feed splits a longer list
 * into several reads; the relay reader refuses one before it is sealed.
 */
export const MAX_SQUAD_EVENT_CONVERSATIONS = 50
export const MAX_SQUAD_EVENT_ID_BYTES = 128
/**
 * How many conversations one batched launch-snapshot read may name
 * (`store.MaxSquadSnapshotConversations`, `squad.snapshot_conversations`);
 * the row snapshots are read in groups of this many.
 */
export const MAX_SQUAD_SNAPSHOT_CONVERSATIONS = 50

const encoder = new TextEncoder()

/**
 * Whether the daemon accepts `id` in a `conversation=` filter
 * (`squadEventConversations` and `printableID`,
 * internal/transport/http/squad_events.go): non-empty, no comma, at most
 * `MAX_SQUAD_EVENT_ID_BYTES` of UTF-8, no space or control character.
 */
export function squadEventIDValid(id: string): boolean {
  return !!id && !id.includes(",") && encoder.encode(id).length <= MAX_SQUAD_EVENT_ID_BYTES && !/[\s\p{C}]/u.test(id)
}
