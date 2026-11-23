import { Fragment, type ReactNode } from "react"
import type { SessionBotDispatch } from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { BotAvatar } from "../project-assistants/BotAvatar.js"
import { visualBranches } from "./order.js"

export type RowTree = { depth: number; branchThrough: boolean; ancestorThrough: boolean }
export type DispatchEntry = {
  id: string; parent?: string; dispatch?: SessionBotDispatch; render: (tree?: RowTree) => ReactNode
}

type Group = { key: string; dispatch: SessionBotDispatch; entries: DispatchEntry[] }

export function renderBotDispatchGroups(rows: DispatchEntry[]) {
  if (!rows.some(row => row.dispatch)) return rows.map(row => row.render())

  const groups = new Map<string, Group>()
  const placements = new Map<string, Group | undefined>()
  const depths = new Map<string, number>()
  const ungrouped: DispatchEntry[] = []
  for (const row of rows) {
    const parentGroup = row.parent ? placements.get(row.parent) : undefined
    let group = parentGroup
    if (row.dispatch) {
      const bot = row.dispatch.dispatcher
      // Older machines have no dispatcher ID. A shared name does not prove identity.
      const key = bot ? JSON.stringify([bot.machine_id, bot.bot_id]) : JSON.stringify([row.id])
      group = groups.get(key)
      if (!group) {
        group = { key, dispatch: row.dispatch, entries: [] }
        groups.set(key, group)
      }
    }
    placements.set(row.id, group)
    const parentDepth = row.parent ? depths.get(row.parent) : undefined
    depths.set(row.id, group === parentGroup && parentDepth !== undefined ? Math.min(parentDepth + 1, 2) : 0)
    if (group) group.entries.push(row)
    else ungrouped.push(row)
  }

  const render = (entry: DispatchEntry, branches: ReturnType<typeof visualBranches>) => entry.render({
    depth: depths.get(entry.id) ?? 0,
    branchThrough: branches.through.has(entry.id), ancestorThrough: branches.ancestorThrough.has(entry.id),
  })
  const branches = visualBranches(ungrouped, depths)
  return rows.map(row => {
    const group = placements.get(row.id)
    if (!group) return render(row, branches)
    if (group.entries[0] !== row) return null

    const { name, appearance } = group.dispatch
    const attribution = name ? nextWord("sessionBotDispatchBy", { bot: name }) : nextWord("sessionBotDispatch")
    const groupBranches = visualBranches(group.entries, depths)
    return <li className="session-list-group session-bot-dispatch-group" key={group.key} role="presentation">
      <div className="session-list-group-heading">
        <BotAvatar appearance={appearance ?? { shape: "blob", color: "#9ca6b3" }} size={24} />
        <strong>{name || attribution}</strong>
      </div>
      <ul className="rows" role="group" aria-label={attribution}>{group.entries.map(entry =>
        <Fragment key={entry.id}>
          {entry.dispatch?.task_title && <li className="session-bot-task-title" role="presentation">{entry.dispatch.task_title}</li>}
          {render(entry, groupBranches)}
        </Fragment>
      )}</ul>
    </li>
  })
}
