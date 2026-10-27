type PreviewHistory = Pick<History, "state" | "pushState" | "replaceState">

/** Reuse a closed modal step; closing must never schedule a later history traversal. */
export function enterVideoHistory(history: PreviewHistory, owner: string): void {
  if (history.state?.videoPreview === owner) return
  const state = { ...history.state, videoPreview: owner }
  delete state.videoPreviewClosed
  if (history.state?.videoPreviewClosed === owner) history.replaceState(state, "")
  else history.pushState(state, "")
}

export function closeVideoHistory(history: PreviewHistory, owner: string): void {
  if (history.state?.videoPreview !== owner) return
  const state = { ...history.state, videoPreviewClosed: owner }
  delete state.videoPreview
  history.replaceState(state, "")
}
