import { richText } from "./markdown.js";

/** Documents and transcript turns share one escaped, fail-visible Markdown implementation. */
export function documentBodyHTML(answer) {
    return richText(answer && answer.text);
}
