/**
 * Attention belongs to the work that explains it. These small selectors keep
 * every question inside its item and every proposal inside its Project.
 */
export function decisionsForWorkItem<T extends { work_id: string | null }>(rows: T[], workID: string): T[] {
  return rows.filter((row) => row.work_id === workID)
}

export function proposalsForProject<T extends { project_id: string }>(rows: T[], projectID: string): T[] {
  return projectID ? rows.filter((row) => row.project_id === projectID) : rows
}
