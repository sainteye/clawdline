// The overlays' public surface: `App` mounts and hosts them, the detail head
// and the status line open them through the events.
export { Overlays, closeKeys, shown, toggleKeys } from "./Overlays.js"
export { Info, hostInfo } from "./info.js"
export { ActionConfirm, endedIfGone, hostConfirm } from "./action-confirm.js"
export { SessionFacts } from "./facts.js"
export {
  GO_PAGE,
  OPEN_CONFIRM,
  OPEN_INFO,
  getClosingId,
  requestConfirm,
  requestInfo,
  requestPage,
  useClosingId,
  type ConfirmRequest,
  type PageRequest,
} from "./events.js"
export { toast, toastFailure } from "./toast.js"
