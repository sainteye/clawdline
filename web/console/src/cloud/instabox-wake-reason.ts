// The catalog word for a Cloud API InstaBox error code. Kept free of imports so
// node can test it without the console's module resolution.
export function instaBoxWakeReasonWord(code: string) {
  switch (code) {
    case "wake_cooldown": return "msInstaCooldown"
    case "wake_in_progress": return "msInstaInProgress"
    case "wake_unconfirmed": return "msInstaUnconfirmed"
    case "wake_manual_required": return "msInstaManualStart"
    case "wake_key_missing":
    case "wake_key_relink": return "msInstaKeyMissing"
    case "instabox_key_invalid":
    case "instabox_key_too_broad":
    case "instabox_key_refused": return "msInstaKeyRefused"
    case "instabox_key_missing_scope": return "msInstaKeyScope"
    case "instabox_key_expired": return "msInstaKeyExpired"
    case "instabox_key_revoked": return "msInstaKeyRevoked"
    case "instabox_machine_not_found": return "msInstaMachineGone"
    default: return "msInstaFailed"
  }
}
