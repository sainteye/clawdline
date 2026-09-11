import { sameSessionSelection } from "../session/selection.js";

function samePictures(left, right) {
    if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false;
    for (var i = 0; i < left.length; i++) if (left[i] !== right[i]) return false;
    return true;
}

/** Whether a delivered request still owns the currently visible composer payload. */
export function deliveredComposerPayloadMatches(submitted, current) {
    return !!submitted && !!current &&
        sameSessionSelection(submitted.identity, current.identity) &&
        submitted.text === current.text &&
        samePictures(submitted.pictures, current.pictures);
}
