import assert from "node:assert/strict";

/*
 * Numbered lists in a transcript, and the one way they go wrong that reads as "the page is
 * broken" rather than "the renderer is imprecise".
 *
 * A numbered list is almost always written with a blank line between its items — that is what a
 * model emits, and it is what a person types when the items are a sentence long each. The web
 * renderer treated every blank line as the end of a block, so a six-step list closed and reopened
 * an `<ol>` six times, and an `<ol>` counts from one. On screen: six steps, all of them "1.".
 *
 * `Markdown.swift` has had the rule since it renumbers list markers itself — "blank lines make a
 * Markdown list loose; they do not start a new list", asserted in `Tests/MarkdownTests.swift`
 * under *ordered numbering continues across loose-list blank lines*. The two surfaces are meant
 * to read the same document the same way; this file is that contract for the page.
 *
 * What the assertions are written against is the *markup*, not the digits: the browser draws the
 * numbers from the `<ol>`, so "one list, opened once" and "starts where the author started" are
 * the whole of what the renderer can get wrong here.
 */

// `core/dom.js` reads every element id when it loads, and `core/util.js` writes into one of them.
const elements = {};
globalThis.document = {
    documentElement: { lang: "en" },
    getElementById: function (id) {
        if (!elements[id]) elements[id] = { textContent: "", className: "", hidden: true };
        return elements[id];
    }
};

const { richText } = await import("../Resources/web/app/js/view/markdown.js");

function count(html, needle) {
    return html.split(needle).length - 1;
}

/* ---- the bug: a blank line between items ---------------------------------- */

const loose = richText("1. first\n\n1. second\n\n1. third");
assert.equal(count(loose, "<ol"), 1,
    "a list written with blank lines between its items is one list, not one list per item");
assert.equal(count(loose, "<li>"), 3, "and it still has all three items");
assert.equal(loose, "<ol><li>first</li><li>second</li><li>third</li></ol>",
    "so the browser numbers them 1, 2, 3 — the whole of the reported bug");

const tight = richText("1. first\n1. second\n1. third");
assert.equal(tight, loose, "spacing between items is presentation; the list is the same list");

const manyBlanks = richText("1. first\n\n\n\n1. second");
assert.equal(count(manyBlanks, "<ol"), 1, "a wider gap is still a gap inside one list");

/* ---- what a blank line still ends ----------------------------------------- */

const interrupted = richText("1. first\n\nplain prose\n\n3. third");
assert.equal(count(interrupted, "<ol"), 2,
    "prose between two lists still ends the first one: only a list item continues a list");
assert.ok(interrupted.includes("<p>plain prose</p>"), "and the prose is a paragraph, not an item");
assert.ok(interrupted.includes("<ol start=\"3\">"),
    "the list after it counts from the number the author wrote, not from one again");

const trailing = richText("1. first\n\ntext after the list");
assert.equal(count(trailing, "<ol"), 1, "a list at the end of a paragraph's line of sight");
assert.ok(trailing.endsWith("<p>text after the list</p>"), "and the text is outside it");

/* ---- where an ordered list starts counting -------------------------------- */

assert.ok(richText("3. three\n4. four").startsWith("<ol start=\"3\">"),
    "an author who numbered from three gets a list that starts at three");
assert.ok(!richText("1. one\n2. two").includes("start="),
    "and the ordinary case carries no attribute at all: one is what an `<ol>` does anyway");
assert.ok(!richText("- one\n- two").includes("start="), "a bullet list has nothing to start");
assert.equal(richText("2. two\n1. one"), "<ol start=\"2\"><li>two</li><li>one</li></ol>",
    "only the first written number is read; the rest count on from it, as `Markdown.swift` does");

/* ---- bullets and numbers are not the same list ---------------------------- */

const mixed = richText("- bullet\n\n1. numbered");
assert.equal(mixed, "<ul><li>bullet</li></ul><ol><li>numbered</li></ol>",
    "kept in one element, the numbered item would draw with a bullet and lose its number");
assert.equal(richText("- bullet\n1. numbered"), mixed,
    "and with no blank line between them either — which is where that number used to disappear");

const bullets = richText("- a\n\n- b");
assert.equal(bullets, "<ul><li>a</li><li>b</li></ul>",
    "a loose bullet list is one list too — the blank-line rule is not about digits");

/* ---- the shapes the fix had to leave alone -------------------------------- */

const nested = richText("1. one\n\n   - sub\n\n2. two");
assert.equal(nested, "<ol><li>one<ul><li>sub</li></ul></li><li>two</li></ol>",
    "a nested list still nests, and the outer list still carries on past it");

const wrapped = richText("1. one\n   continued here\n\n2. two");
assert.equal(wrapped, "<ol><li>one continued here</li><li>two</li></ol>",
    "a wrapped continuation line still belongs to the item above it");

const fenced = richText("1. one\n\n```\ncode\n```\n\n1. one again");
assert.equal(count(fenced, "<ol"), 2, "a code block between items is a block, and ends the list");
assert.ok(fenced.includes("<pre class=\"code\">code</pre>"), "and is still rendered as one");

const heading = richText("- item\n\n## a heading");
assert.ok(heading.includes("<h4>a heading</h4>"), "a heading after a list is still a heading");
assert.equal(count(heading, "<li>"), 1, "and did not get eaten as an item");

assert.equal(richText(""), "", "nothing in, nothing out");

console.log("web-markdown-lists: ok");
