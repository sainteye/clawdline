import type { DevStack, DevStacksReply } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { stackTipSaid } from "./words.js"

/**
 * The server list (⌘S): `Controller.refreshStacks` and `stackRowText`, drawn
 * from `GET /v1/devstacks`.
 *
 * What a row says is the Swift row's, piece for piece, for the pieces this
 * daemon can know: the project's name in its accent, `▮ up/total` in green
 * when every declared port answers and in red when only some do, `▯ 0/total`
 * when none does, and each answering process as `name:port` and its address
 * as `↗ host`. How long a stack has been up, and which process broke and why,
 * come from a `status` command in the Swift app; this daemon runs none of a
 * project's commands (devstacks.schema.json), so those two are not drawn, and
 * a stack only its `status` command could describe says so in words where the
 * Swift row said "not trusted yet".
 *
 * The Swift row's buttons — trust, start, restart, stop, logs — each ran one
 * of the project's own commands. They are not drawn at all rather than drawn
 * dead; the list's last line says why, and where to do it instead.
 */

/** "A panel of servers that stops updating while you look at it is a panel you have to close and reopen to trust." */
export const STACKS_REFRESH_MS = 5000

/** One read of the list. Any failure is a list that could not be read, said as such. */
export async function readStacks(): Promise<DevStacksReply> {
  const res = await fetch("/v1/devstacks")
  if (!res.ok) throw new Error("devstacks answered " + res.status)
  const reply = (await res.json()) as DevStacksReply
  if (!reply || !Array.isArray(reply.stacks)) throw new Error("devstacks answered without a list")
  return reply
}

/** `upCount`: how many are up out of how many are declared. */
export function upCount(stack: DevStack): number {
  return stack.processes.filter((p) => p.state === "running" || p.state === "healthy" || p.state === "completed")
    .length
}

/** The mark and count, or the words for a stack nothing here can ask about. */
export function stackStateSaid(stack: DevStack): { text: string; tone: "ok" | "bad" | "off" } {
  const total = stack.processes.length
  switch (stack.state) {
    case "running":
      return { text: `▮ ${upCount(stack)}/${total}`, tone: "ok" }
    case "partial":
      return { text: `▮ ${upCount(stack)}/${total}`, tone: "bad" }
    case "stopped":
      return { text: `▯ 0/${total}`, tone: "off" }
    default:
      // In words, not a mark: "a grey square beside a green one reads as
      // 'down'", and this project is very likely up.
      return {
        text: "▨ " + (stack.unknown === "status_not_run" ? nextWord("stackStatusNotRun") : nextWord("stackNothingDeclared")),
        tone: "off",
      }
  }
}

/** The row's hover: `stackTip`, or why nothing could be asked. */
export function stackTip(stack: DevStack): string {
  if (stack.state === "unknown") return nextWord("stackUnknownTip", { root: stack.root })
  return stackTipSaid(upCount(stack), stack.processes.length)
}

/** Each place an answering process can be opened: its port, then the address the file gives it. */
export function stackLinks(stack: DevStack): { ports: { label: string; href: string }[]; hosts: { label: string; href: string }[] } {
  const up = stack.processes.filter((p) => p.state === "running" || p.state === "healthy" || p.state === "completed")
  const ports = up
    .filter((p) => typeof p.port === "number" && p.port > 0)
    .map((p) => ({ label: `${p.name}:${p.port}`, href: `http://localhost:${p.port}` }))
  const hosts = up
    .filter((p) => !!p.url && /^https?:\/\//i.test(p.url))
    .map((p) => {
      let host = p.url as string
      try {
        host = new URL(host).hostname || host
      } catch {
        /* the address as written */
      }
      return { label: host, href: p.url as string }
    })
  return { ports, hosts }
}
