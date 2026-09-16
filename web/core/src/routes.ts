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
} as const

export type RouteName = keyof typeof routes
