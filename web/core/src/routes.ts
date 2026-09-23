// Where each contract type lives on the daemon.
//
// One table, so a route spelled in three components cannot be spelled three
// ways. It is still hand-written, which is the remaining half of this problem:
// the daemon's own mux is the other copy, and nothing yet compares them. That
// gap is named in docs/plan.md rather than left to be discovered.
export const routes = {
  health: "/v1/health",
  sessions: "/v1/sessions",
  events: "/v1/events",
  inventory: "/v1/next/sessions",
  obligations: "/v1/next/obligations",
  tasks: "/v1/orchestrator/tasks",
  board: "/v1/board",
  schedules: "/v1/orchestrator/schedules",
  coordinator: "/v1/next/coordinator",
  strings: "/v1/strings",
  transcript: "/v1/transcript",
} as const

/**
 * Routes that act on one session.
 *
 * A terminal id can contain a percent sign — tmux panes are `%19` — so it is
 * encoded here rather than interpolated. Left raw, `%19` reaches the daemon as
 * a control character and comes back `not_found`, which is a true answer to a
 * question nobody asked.
 */
export const sessionRoutes = {
  send: (id: string) => `/v1/sessions/${encodeURIComponent(id)}/send`,
  interrupt: (id: string) => `/v1/sessions/${encodeURIComponent(id)}/interrupt`,
  close: (id: string) => `/v1/sessions/${encodeURIComponent(id)}/close`,
  title: (id: string) => `/v1/sessions/${encodeURIComponent(id)}/title`,
  smartTitle: (id: string) => `/v1/sessions/${encodeURIComponent(id)}/smart-title`,
  agent: (id: string, agent: string) =>
    "/v1/sessions/" + encodeURIComponent(id) + "/agents/" + encodeURIComponent(agent),
} as const

export type RouteName = keyof typeof routes
