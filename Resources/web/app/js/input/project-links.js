const SERVED_PROJECT_ARTIFACT =
    /^\/v1\/sessions\/[^/]+\/artifacts\/(backlog|milestone)$/;

/** Only explicit web URLs and the broker's two typed same-origin artifact slots become anchors. */
export function isOpenableProjectLink(url) {
    return /^https?:\/\//i.test(url || "") || SERVED_PROJECT_ARTIFACT.test(url || "");
}

/** A served artifact has no useful host to print; the authority is Clawdline itself. */
export function isServedProjectArtifact(url) {
    return SERVED_PROJECT_ARTIFACT.test(url || "");
}
