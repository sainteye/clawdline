/**
 * How far along a Session's to-dos are, in the three states a person asks
 * about at a glance: finished, being worked on, not yet started.
 *
 * A Board item is started once its owner moves it past `assigned`; the phases
 * after that are the Session's own work. A direct to-do is started once the
 * Session has it — it read the row, or wrote the row itself — because nothing
 * finer is recorded for one. Finished is what the fold lists as finished: the
 * recently completed Board items and the completed direct to-dos.
 */
export interface TodoProgress {
  done: number
  active: number
  waiting: number
}

const STARTED = new Set(["implementing", "verifying", "merging", "deploying"])

export function todoProgress(page: {
  assigned_items: { phase: string }[]
  recent_items: unknown[]
  direct_todos: { read_at: number | null; completed_at: number | null; created_by?: string }[]
}, conversation: string | undefined): TodoProgress {
  let done = page.recent_items.length
  let active = 0
  let waiting = 0
  for (const item of page.assigned_items) {
    if (STARTED.has(item.phase)) active += 1
    else waiting += 1
  }
  for (const todo of page.direct_todos) {
    if (todo.completed_at) done += 1
    else if (todo.read_at || (!!conversation && todo.created_by === conversation)) active += 1
    else waiting += 1
  }
  return { done, active, waiting }
}

/** The sentence a screen reader hears for the three counts. */
export function todoProgressLabel(p: TodoProgress): string {
  const total = p.done + p.active + p.waiting
  return `${total} 個待辦：${p.done} 個完成、${p.active} 個進行中、${p.waiting} 個未開始`
}
