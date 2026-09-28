/**
 * Plan items are future work, so the Board keeps them out of the current view
 * until the person explicitly asks to see them. The switch is intentionally
 * browser-memory only: a fresh visit returns to the current-work view.
 */
export function visibleWorkItems<T extends { kind: string }>(items: readonly T[], showPlans: boolean): T[] {
  return showPlans ? [...items] : items.filter((item) => item.kind !== "plan")
}
