/** The only two Fast-mode states Clawdline may act from. Unknown is deliberately inert: `/fast`
 * toggles rather than names its destination, so sending it from an unknown start can do the
 * opposite of what the reader intended. */
export function nextFastMode(current) {
    if (current === "standard") return "fast";
    if (current === "fast") return "standard";
    return null;
}

/** Codex's own command. Kept closed here so no value read from a rollout can become terminal
 * input. */
export function fastModeCommand() { return "/fast"; }

/** A send receipt proves only that the line reached the terminal. The request is settled when a
 * later rollout reading names the requested state. */
export function settledFastMode(pending, current) {
    return !pending || pending === current;
}
