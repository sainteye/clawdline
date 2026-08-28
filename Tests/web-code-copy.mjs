import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

/*
 * The copy button on a fenced code block.
 *
 * The one thing here that can be wrong without looking wrong is what reaches the clipboard.
 * The page renders correctly whether the button carries the code or the *markup* of the code,
 * so a paste that says `&amp;` where the transcript showed `&` is invisible on screen and is
 * found only by whoever pastes the command into a shell and watches it not work. That is the
 * assertion this file exists for; everything else around it is the promise that the button
 * never sits on top of the text it is offering to copy.
 */

// `core/dom.js` reads every element id when it loads, and `core/util.js` writes the toast into
// one of them. One stable object per id, so the toast can be read back after a press.
const elements = {};
globalThis.document = {
    documentElement: { lang: "en" },
    getElementById: function (id) {
        if (!elements[id]) elements[id] = { textContent: "", className: "", hidden: true };
        return elements[id];
    }
};

const { richText, codeBlockHTML, copyCodeBlock } =
    await import("../Resources/web/app/js/view/markdown.js");
const { els } = await import("../Resources/web/app/js/core/dom.js");
const { T } = await import("../Resources/web/app/js/core/i18n.js");
const { esc } = await import("../Resources/web/app/js/core/esc.js");

const styles = await readFile(
    new URL("../Resources/web/app/css/transcript.css", import.meta.url),
    "utf8"
);
const transcriptJS = await readFile(
    new URL("../Resources/web/app/js/view/transcript.js", import.meta.url),
    "utf8"
);

/** The five entities `esc` writes, plus the newline the attribute encodes. `&amp;` is undone
 *  last: undo it first and `&amp;lt;` — an ampersand that was in the code — comes back as `<`. */
function decode(value) {
    return String(value)
        .replace(/&#10;/g, "\n").replace(/&#39;/g, "'").replace(/&quot;/g, "\"")
        .replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
}

/** The first value of a double-quoted attribute. Safe as a regex precisely because `esc` has
 *  turned every quote in the payload into `&quot;` — which is the other half of what is
 *  being tested here. */
function attribute(html, name) {
    var found = new RegExp(name + "=\"([^\"]*)\"").exec(html);
    return found ? found[1] : null;
}

function count(html, needle) {
    return html.split(needle).length - 1;
}

/** Node ships a read-only `navigator` of its own, so the browser's has to be installed over
 *  it rather than assigned to it. */
function browserNavigator(value) {
    Object.defineProperty(globalThis, "navigator", { value: value, configurable: true });
}

/* ---- what goes to the clipboard ------------------------------------------ */

const command = "curl -s \"http://127.0.0.1:7717/v1/health\" | jq '.ok & .state' > /tmp/a<b>.txt";
const source = command + "\n  second line\twith a tab";
const html = richText("```bash\n" + source + "\n```");

const copied = decode(attribute(html, "data-code-copy"));
assert.equal(copied, source, "the clipboard gets the code as it arrived, before it was escaped");
assert.ok(!copied.includes("&amp;"), "an ampersand pastes as `&`, not as the entity on screen");
assert.ok(!copied.includes("&quot;"), "and a quote pastes as a quote");
assert.equal(copied.split("\n").length, 2, "a block is copied whole, newlines and all");
assert.ok(!copied.startsWith("bash"), "the language tag on the fence is not content");

// The code on screen is still escaped: the payload above is a second copy, not a loosening.
assert.ok(html.includes("<pre class=\"code\">curl -s &quot;http"),
    "the rendered block is escaped exactly as it was before the button existed");

/* ---- where the button sits ----------------------------------------------- */

assert.ok(html.startsWith("<div class=\"codeblock\">"),
    "the block is wrapped, so the button has a corner to be absolute in");
assert.ok(html.includes("</pre><button"),
    "the button is the `pre`'s sibling, not its child: inside it, it would scroll away with the code");
assert.ok(html.includes("class=\"codecopy\""), "and it is the class the stylesheet places");
assert.equal(count(html, "<button"), 1, "one button for one block");

const two = richText("```\nalpha\n```\n\nprose\n\n```\nbeta\n```");
assert.equal(count(two, "class=\"codecopy\""), 2, "every block gets its own");
assert.equal(decode(attribute(two, "data-code-copy")), "alpha", "each carrying its own block");

/* ---- the code cannot write markup of its own ------------------------------ */

const hostile = "</pre><button class=\"codecopy\" data-code-copy=\"x\">gotcha</button>";
const guarded = richText("```\n" + hostile + "\n```");
assert.equal(decode(attribute(guarded, "data-code-copy")), hostile,
    "a block that contains this page's own markup still copies as text");
assert.equal(count(guarded, "<button"), 1, "and cannot open a second button");
assert.equal(count(guarded, "class=\"codecopy\""), 1);

/* ---- nothing to copy ------------------------------------------------------ */

const empty = richText("```\n```");
assert.ok(empty.includes("<pre class=\"code\"></pre>"), "an empty block still renders");
assert.ok(!empty.includes("codecopy"), "and has nothing to offer a button for");
assert.equal(codeBlockHTML(""), "<pre class=\"code\"></pre>", "same shape it had before");
assert.equal(codeBlockHTML(null), "<pre class=\"code\"></pre>");

const inline = richText("run `ls -l` now");
assert.ok(!inline.includes("codecopy"), "inline code is not a block and keeps its shape");
assert.ok(inline.includes("<code>ls -l</code>"));

/* ---- the words on it ------------------------------------------------------ */

for (const key of ["webCodeCopy", "webCodeCopied", "webCodeCopyFailed"]) {
    assert.equal(typeof T[key], "string", key + " has an English fallback in core/i18n.js");
    assert.ok(T[key].length > 0, key + " is not blank");
}
assert.notEqual(T.webCodeCopied, T.webCodeCopyFailed, "a refusal does not read as a success");
assert.ok(html.includes("title=\"" + esc(T.webCodeCopy) + "\""), "the button names itself on hover");
assert.ok(html.includes("aria-label=\"" + esc(T.webCodeCopy) + "\""), "and to a screen reader");
assert.ok(html.includes("<svg"), "the mark is drawn, not typed: a phone font need not carry the glyph");

/* ---- pressing it ---------------------------------------------------------- */

const written = [];
browserNavigator({
    clipboard: {
        writeText: function (text) { written.push(text); return Promise.resolve(); }
    }
});
await copyCodeBlock(source);
assert.deepEqual(written, [source], "the press writes the block, unescaped, once");
assert.equal(els.toast.textContent, T.webCodeCopied, "and says so");
assert.equal(els.toast.className, "toast");

browserNavigator({
    clipboard: { writeText: function () { return Promise.reject(new Error("denied")); } }
});
els.toast.textContent = "";
await copyCodeBlock(source);
assert.equal(els.toast.textContent, T.webCodeCopyFailed, "a rejected write is not silent");
assert.equal(els.toast.className, "toast err", "and is not dressed as a success");

// A page served over plain http — which is how this one is read on a home network — has no
// clipboard object at all. The info card returns silently there; this button must not.
browserNavigator({});
els.toast.textContent = "";
await copyCodeBlock(source);
assert.equal(els.toast.textContent, T.webCodeCopyFailed, "a browser with no clipboard says so too");
assert.equal(els.toast.className, "toast err");

els.toast.textContent = "";
await copyCodeBlock("");
assert.equal(els.toast.textContent, "", "and an empty press says nothing at all");

/* ---- the transcript hands presses over -------------------------------------- */

assert.ok(/closest\("button\.codecopy"\)/.test(transcriptJS),
    "the transcript's own click handler recognises the button");
assert.ok(/copyCodeBlock/.test(transcriptJS), "and hands it to the clipboard path");

/* ---- the button does not cover what it is offering ------------------------- */

assert.match(styles, /\.entry \.body pre\.code \{[^}]*overflow-x: auto/,
    "the block still scrolls sideways: the button is not paid for by hiding the overflow");
assert.match(styles, /\.entry \.body \.codeblock \{[^}]*position: relative/,
    "the wrapper is what the button is positioned against");
assert.match(styles, /\.entry \.body \.codeblock > pre\.code \{[^}]*padding-right/,
    "and the gutter is what keeps the last character clear of it at full scroll");
assert.match(styles, /\.entry \.body \.codecopy \{[^}]*position: absolute/,
    "the button is out of flow, so the block is as wide as it ever was");
assert.match(styles, /background-attachment: local/,
    "the edge shading that says there is more to the right of a scrolled block");

console.log("web-code-copy: ok");
