import { nextWord } from "../next-strings.js"

const reasons = {
  within_grace: "machineReclaimReasonWithinGrace",
  owner_present: "machineReclaimReasonOwnerPresent",
  owner_unknown: "machineReclaimReasonOwnerUnknown",
  path_not_owned: "machineReclaimReasonPathNotOwned",
  unreadable: "machineReclaimReasonUnreadable",
  nested_repository: "machineReclaimReasonNestedRepository",
  filters_present: "machineReclaimReasonFiltersPresent",
  preserve_failed: "machineReclaimReasonPreserveFailed",
  changed_during_sweep: "machineReclaimReasonChangedDuringSweep",
  intent_not_recorded: "machineReclaimReasonIntentNotRecorded",
  remove_failed: "machineReclaimReasonRemoveFailed",
} as const

export function reclaimReasonWord(code: string): string {
  if (!Object.hasOwn(reasons, code)) return nextWord("machineStorageUnknown")
  return nextWord(reasons[code as keyof typeof reasons])
}
