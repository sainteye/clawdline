import type { WorkV2Item } from "./api.js"

/** Board edits that name their base, and `field_conflict`, arrive with the Bot settings rebase at API 117. */
export const WORK_V2_EDIT_BASE_API_LEVEL = 117

export interface WorkV2EditBase {
  base: WorkV2Item
  overwrite?: boolean
  /** The machine's `api_level`; unknown counts as older. */
  apiLevel?: number
}

/**
 * The PATCH body for a person's Board edit. A machine older than API 117
 * decodes this route strictly and refuses a body carrying `base`, so it gets
 * the earlier shape: the live item's version and the edited fields only.
 */
export function editWorkV2Body(item: WorkV2Item, title: string, description: string, acceptance?: string, based?: WorkV2EditBase) {
  const rebase = based && (based.apiLevel ?? 0) >= WORK_V2_EDIT_BASE_API_LEVEL ? based : undefined
  return {
    expected_version: (rebase?.base ?? item).version,
    title,
    description,
    ...(acceptance === undefined ? {} : { acceptance_criteria: acceptance }),
    ...(rebase ? {
      base: { title: rebase.base.title, description: rebase.base.description,
        ...(acceptance === undefined ? {} : { acceptance_criteria: rebase.base.acceptance_criteria ?? "" }) },
      ...(rebase.overwrite ? { overwrite_conflicts: true } : {}),
    } : {}),
  }
}
