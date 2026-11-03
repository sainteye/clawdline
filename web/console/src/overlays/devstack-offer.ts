import type { ProjectLink, ProjectServerSetup } from "@clawdline/contract"

/**
 * The button under the Links section: start this project's dev stack, or set
 * one up when there is none.
 *
 * **The daemon still runs nothing the file names** (devstacks.go). The press
 * sends one request into this Session, and its own assistant starts the stack
 * or writes `.devstack.json` with the permissions the person already gave it.
 *
 * A module of its own, as `links-note.ts` is, because `info.ts` cannot be
 * loaded by `node --test` and this is the part with the decision in it.
 */
export type DevStackOffer = "start" | "create"

/**
 * `devStack` is the daemon's static reading of the nearest `.devstack.json`.
 * A readable file is something to start, even one that declares no ports: its
 * `up` command may still start the stack. A missing or unreadable file is
 * something to set up or repair. An older daemon sends no reading, and then a
 * server row is the only evidence that a file exists.
 */
export function devStackOffer(devStack: ProjectServerSetup | undefined, links: ProjectLink[]): DevStackOffer {
  if (devStack === "ready" || devStack === "empty") return "start"
  if (devStack === "missing" || devStack === "attention") return "create"
  return links.some(link => link.kind === "server") ? "start" : "create"
}

type Word = "stackStartButton" | "stackCreateButton" | "stackStartRequest" | "stackCreateRequest" | "stackStartSent" | "stackCreateSent"

const WORDS: Record<DevStackOffer, { button: Word; request: Word; sent: Word }> = {
  start: { button: "stackStartButton", request: "stackStartRequest", sent: "stackStartSent" },
  create: { button: "stackCreateButton", request: "stackCreateRequest", sent: "stackCreateSent" },
}

/** The catalog keys for an offer's label, the request it sends and the line said after sending. */
export function devStackWords(offer: DevStackOffer): { button: Word; request: Word; sent: Word } {
  return WORDS[offer]
}
