/**
 * Attention belongs to the work that explains it. These small selectors keep
 * the Board's normal path (a question inside its item) separate from the
 * compatibility path for a persisted question that predates that invariant.
 */
export function decisionsForWorkItem<T extends { work_id: string | null }>(rows: T[], workID: string): T[] {
  return rows.filter((row) => row.work_id === workID)
}

export function unattachedDecisions<T extends { work_id: string | null }>(rows: T[]): T[] {
  return rows.filter((row) => !row.work_id)
}

export function proposalsForProject<T extends { project_id: string }>(rows: T[], projectID: string): T[] {
  return projectID ? rows.filter((row) => row.project_id === projectID) : rows
}
