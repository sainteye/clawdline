/* --------------------------------------------------------------------------
   What goes on the end of what is already in the box

   One rule, in a file of its own, because three things now depend on it being
   the same rule: dictation, a snippet, and whatever the next thing to write
   into the composer turns out to be. It used to be two lines inside
   `appendMsg`, which made it correct and unreadable from anywhere else — a
   second insertion path could only re-derive it, and a re-derived join rule is
   one that drifts the first time somebody changes half of it.

   Nothing here touches the document, so `Tests/web-snippets.mjs` can state the
   rule as a table instead of asserting it against a `contenteditable`.
   -------------------------------------------------------------------------- */

/**
 * The gap between what is in the box and what is about to be added: one space, or nothing.
 *
 * Nothing when the box is empty — a leading space in front of the first word is a space
 * somebody has to notice and delete — and nothing when what is there already ends in
 * whitespace, because the person typing put that space there themselves and a second one is
 * not an improvement. A space otherwise, so a sentence dictated after a typed one is a sentence
 * and not one long word.
 */
export function appendGap(existing) {
    var had = String(existing == null ? "" : existing);
    return had && !/\s$/.test(had) ? " " : "";
}

/** The whole box after the addition, for the path that rewrites it rather than typing into it. */
export function appendedText(existing, addition) {
    var had = String(existing == null ? "" : existing);
    return had + appendGap(had) + String(addition == null ? "" : addition);
}
