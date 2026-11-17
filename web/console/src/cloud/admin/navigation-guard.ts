// An issued code is readable only until this page closes. Keep the decision
// beside the route handler so links and browser history ask the same question.
let pendingCode: (() => boolean | Promise<boolean>) | null = null

export function guardCodeNavigation(confirmLeave: () => boolean | Promise<boolean>): () => void {
  pendingCode = confirmLeave
  return () => {
    if (pendingCode === confirmLeave) pendingCode = null
  }
}

export async function mayLeaveCode(): Promise<boolean> {
  const guard = pendingCode
  return guard ? await guard() && pendingCode === guard : true
}
