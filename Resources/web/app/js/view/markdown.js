import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";
import { toast } from "../core/util.js";

/* --------------------------------------------------------------------------
   Markdown, to the same depth the bar's own pane reads it
   ------------------------------------------------------------------------ */

/**
 * Enough Markdown to read a Claude Code answer, and no more.
 *
 * The recognisers are ported from ``Markdown.swift`` so the two surfaces agree about what a
 * table is and what a heading is — a document that draws as a table on the Mac and as a row of
 * pipes here would mean one of them is lying about the same text. Its rule about failure is
 * ported too: **anything unrecognised falls through as plain text.** A stray asterisk on screen
 * is a blemish; a sentence that disappeared into a parser is a bug you cannot see.
 *
 * Everything is escaped before a single tag is written. This text came off somebody's terminal,
 * and a terminal will carry any angle bracket you put in front of it.
 */
export function richText(text) {
    if (!text) return "";
    var lines = String(text).replace(/\r\n?/g, "\n").split("\n");
    var out = [];
    var i = 0;

    while (i < lines.length) {
        var line = lines[i];
        var trimmed = line.trim();

        // Fenced code. The opening line may carry a language tag, which is not content.
        if (trimmed.indexOf("```") === 0) {
            var body = [];
            i += 1;
            while (i < lines.length && lines[i].trim().indexOf("```") !== 0) { body.push(lines[i]); i += 1; }
            i += 1;
            out.push(codeBlockHTML(body.join("\n")));
            continue;
        }

        if (!trimmed) { i += 1; continue; }

        if (isRule(trimmed)) { out.push("<hr>"); i += 1; continue; }

        var head = heading(trimmed);
        if (head) {
            // h1 and h2 in a transcript entry would outweigh the page's own furniture, so the
            // levels are shifted down: what matters is that six of them stay distinguishable.
            var tag = "h" + Math.min(6, head[0] + 2);
            out.push("<" + tag + ">" + inlineMd(head[1]) + "</" + tag + ">");
            i += 1;
            continue;
        }

        // A table is a run of piped lines *under a separator row*. Detected as a block rather
        // than line by line, or the row of dashes lands in the document as a paragraph.
        if (looksLikeTableRow(trimmed) && i + 1 < lines.length && isTableSeparator(lines[i + 1].trim())) {
            var rows = [];
            while (i < lines.length && looksLikeTableRow(lines[i].trim())) { rows.push(lines[i].trim()); i += 1; }
            out.push(tableHTML(rows));
            continue;
        }

        if (trimmed.charAt(0) === ">") {
            var quoted = [];
            while (i < lines.length && lines[i].trim().charAt(0) === ">") {
                quoted.push(lines[i].trim().slice(1).trim());
                i += 1;
            }
            out.push("<blockquote>" + inlineMd(quoted.join(" ")) + "</blockquote>");
            continue;
        }

        if (listItem(line)) {
            var block = [];
            while (i < lines.length && (listItem(lines[i]) || (block.length && lines[i].trim() && !isBlockStart(lines[i])))) {
                block.push(lines[i]);
                i += 1;
            }
            out.push(listHTML(block));
            continue;
        }

        // Paragraph: gather until something else starts. Single newlines inside one are a wrapped
        // line and not a break, which is how the source was written and how the bar reads it.
        var paragraph = [trimmed];
        i += 1;
        while (i < lines.length) {
            var next = lines[i].trim();
            if (!next || isBlockStart(lines[i])) break;
            paragraph.push(next);
            i += 1;
        }
        out.push("<p>" + inlineMd(paragraph.join(" ")) + "</p>");
    }
    return out.join("");
}

/**
 * A fenced block, and the button that puts it on the clipboard.
 *
 * A command in a transcript is there to be run somewhere else, and until this existed the only
 * way to get one out of the page was to select it by hand — inside a box that scrolls sideways,
 * on a phone. So the block gets a wrapper, and the button is a child of the *wrapper* rather
 * than of the `<pre>`: inside the `<pre>` it would scroll away with the code it belongs to.
 *
 * The text is carried a second time in `data-code-copy`, which is the shape the info card's
 * copy buttons already have (`data-copy`, `input/info.js`). What reaches the clipboard is
 * therefore this string — the code as it arrived, before a character was escaped — and never
 * the markup. That distinction is invisible on screen, which is why `Tests/web-code-copy.mjs`
 * pins it: a block renders identically whether the button carries `&` or `&amp;`, and the
 * difference only appears in whatever the reader pastes it into.
 *
 * Newlines are written as `&#10;` deliberately. A raw newline inside an attribute does survive
 * the HTML parser, but it survives by a rule about attribute-value normalisation that nothing
 * else on this page depends on — and a four-line snippet arriving as one long line would be a
 * wrong answer nobody sees until it is in a shell.
 *
 * An empty block has nothing to copy, and gets exactly the markup it had before.
 */
export function codeBlockHTML(code) {
    var text = String(code == null ? "" : code);
    var block = '<pre class="code">' + esc(text) + "</pre>";
    if (!text) return block;
    return '<div class="codeblock">' + block +
        '<button type="button" class="codecopy" data-code-copy="' +
        esc(text).replace(/\n/g, "&#10;") + '" title="' + esc(T.webCodeCopy) +
        '" aria-label="' + esc(T.webCodeCopy) + '">' +
        // Drawn rather than typed, for the reason `input/info.js` gives beside the same two
        // rectangles: the glyphs for "copy" live in a Unicode block a phone's system font need
        // not carry, and a tofu box on the only button over a command is worse than no button.
        '<svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">' +
        '<rect x="5.25" y="1.75" width="9" height="9" rx="2"></rect>' +
        '<rect x="1.75" y="5.25" width="9" height="9" rx="2"></rect></svg></button></div>';
}

/**
 * The clipboard, reached the way `input/info.js` reaches it — with one deliberate difference.
 *
 * There, a browser with no `navigator.clipboard` and a rejected write are both silent. Here
 * they say so. This button is the whole of the answer to "how do I get this command out of the
 * page", and a press that produces nothing at all cannot be told apart from a page that has
 * stopped working. The absent-clipboard branch is not hypothetical: the object is missing on
 * any page served over plain http, which is how this one is read on a home network.
 *
 * Returns the promise so a test can wait for the answer; nothing in the page awaits it.
 */
export function copyCodeBlock(text) {
    if (typeof text !== "string" || !text) return Promise.resolve();
    if (!navigator.clipboard) { toast(T.webCodeCopyFailed, true); return Promise.resolve(); }
    return navigator.clipboard.writeText(text).then(function () {
        toast(T.webCodeCopied);
    }).catch(function () { toast(T.webCodeCopyFailed, true); });
}

function isBlockStart(line) {
    var t = line.trim();
    return !t || t.indexOf("```") === 0 || t.charAt(0) === ">" || !!heading(t)
        || !!listItem(line) || isRule(t) || looksLikeTableRow(t);
}

function heading(line) {
    var m = /^(#{1,6})\s+(.*)$/.exec(line);
    return m ? [m[1].length, m[2].trim()] : null;
}

/** The marker and the text, plus how far it is indented — the indent is what makes a nested
 *  list nested, and it is the one thing the bar's flat list cannot show. */
function listItem(line) {
    var m = /^(\s*)([-*+])\s+(.*)$/.exec(line);
    if (m) return { indent: m[1].length, ordered: false, marker: "", text: m[3] };
    m = /^(\s*)(\d{1,3})\.\s+(.*)$/.exec(line);
    if (m) return { indent: m[1].length, ordered: true, marker: m[2], text: m[3] };
    return null;
}

function isRule(line) {
    return /^(-{3,}|_{3,}|\*{3,})$/.test(line);
}

function looksLikeTableRow(line) {
    return line.indexOf("|") >= 0 && (line.match(/\|/g) || []).length >= 2;
}

function isTableSeparator(line) {
    return looksLikeTableRow(line) && /^[|\-: ]+$/.test(line);
}

/** The cells of one row. A leading and a trailing pipe are borders, not empty cells. */
function tableCells(row) {
    var parts = row.split("|");
    if (!parts[0].trim()) parts.shift();
    if (parts.length && !parts[parts.length - 1].trim()) parts.pop();
    return parts.map(function (c) { return c.trim(); });
}

/**
 * A real `<table>`, which is the whole reason this page can do better than the bar.
 *
 * The pane on the Mac had to build one out of `NSTextTable` after two simpler attempts failed —
 * pipes left in and set in monospace do not line up, because a CJK glyph comes from a fallback
 * face whose advance is not reliably twice the monospace one. A browser has actual table layout,
 * so the columns are the browser's problem and the alignment markers can be honoured on top.
 *
 * Wide tables scroll inside their own box. A table is the one thing in a transcript that can be
 * genuinely wider than the reading column, and the alternative — a page that scrolls sideways —
 * breaks every other line on the screen to rescue one.
 */
function tableHTML(rows) {
    var separator = -1;
    for (var r = 0; r < rows.length; r++) { if (isTableSeparator(rows[r])) { separator = r; break; } }
    var align = separator >= 0 ? tableCells(rows[separator]).map(function (spec) {
        var left = spec.charAt(0) === ":", right = spec.charAt(spec.length - 1) === ":";
        return left && right ? "center" : right ? "right" : left ? "left" : "";
    }) : [];

    var grid = rows.filter(function (row) { return !isTableSeparator(row); }).map(tableCells);
    if (!grid.length) return "";
    var columns = 0;
    grid.forEach(function (row) { columns = Math.max(columns, row.length); });
    if (!columns) return "";

    function cell(tag, value, c) {
        var style = align[c] ? ' style="text-align:' + align[c] + '"' : "";
        return "<" + tag + style + ">" + inlineMd(value || "") + "</" + tag + ">";
    }
    var html = '<div class="tablewrap"><table>';
    // A header row only when the separator says so — the first row of a table with no separator
    // above it is data, and promoting it would be inventing a heading.
    var start = 0;
    if (separator === 1) {
        html += "<thead><tr>";
        for (var c = 0; c < columns; c++) html += cell("th", grid[0][c], c);
        html += "</tr></thead>";
        start = 1;
    }
    html += "<tbody>";
    for (var g = start; g < grid.length; g++) {
        html += "<tr>";
        for (var k = 0; k < columns; k++) html += cell("td", grid[g][k], k);
        html += "</tr>";
    }
    return html + "</tbody></table></div>";
}

/** A run of list lines, nested by how far each one is indented. */
function listHTML(block) {
    var items = [];
    block.forEach(function (line) {
        var item = listItem(line);
        if (item) { items.push(item); return; }
        // A wrapped continuation line belongs to the item above it.
        if (items.length) items[items.length - 1].text += " " + line.trim();
    });
    if (!items.length) return "";

    var html = "";
    var stack = [];            // the indents currently open, outermost first
    items.forEach(function (item) {
        while (stack.length && item.indent < stack[stack.length - 1].indent) {
            html += "</li></" + stack.pop().tag + ">";
        }
        if (!stack.length || item.indent > stack[stack.length - 1].indent) {
            var tag = item.ordered ? "ol" : "ul";
            if (stack.length) html += "<" + tag + ">";       // opens inside the item above
            else html += "<" + tag + ">";
            stack.push({ indent: item.indent, tag: tag });
        } else {
            html += "</li>";
        }
        html += "<li>" + inlineMd(item.text);
    });
    while (stack.length) html += "</li></" + stack.pop().tag + ">";
    return html;
}

/**
 * `**bold**`, `*italic*`, `_italic_`, `` `code` ``, `~~struck~~`, `[text](url)`,
 * `<https://url>`.
 *
 * Unmatched markers stay as text, the same rule the bar follows. The one thing this does that
 * the bar does not have to think about is *where a link may point*: a transcript is somebody
 * else's output, so only the three schemes that cannot execute anything are turned into links.
 * Absolute file paths are a fourth recognised shape, but are labels rather than anchors: they
 * name a file on the Mac, which a phone browser cannot open.
 */
export function inlineMd(text) {
    var s = esc(text);

    // Code spans come out of the string first and go back in last.
    //
    // Splitting on backticks and running the emphasis rules over each piece separately looks
    // equivalent to this and is not. In `**turn on `remote_write` first**` the opening `**` and
    // the closing `**` land in different pieces, so neither one matches, and the reader is shown
    // literal asterisks around the exact sentence somebody was trying to make emphatic — which
    // is how a real answer came to read `**在 Cloudflare Zero Trust 幫 `host` 掛一層 Access**`.
    //
    // The stand-in is `<0>`, `<1>`, …, and the angle brackets are what make it safe: `esc` above
    // has already turned every `<` in the source into `&lt;`, so the only ones left in the string
    // are the ones put here, and nothing the source can say will collide with them. It is also
    // inert to every rule below, carrying no `*`, `_`, `~`, `[` or `(` for them to match on.
    // What goes back is the span's escaped text and nothing else: whatever was inside a code
    // span was never markup, and never met a rule that could have read it as any.
    var spans = [];
    s = s.replace(/`([^`]*)`/g, function (all, code) {
        // An unclosed backtick is a backtick, not the start of code that never ends: this needs
        // a closing one to match, so a lone backtick is left standing where it was written.
        spans.push(code);
        return "<" + (spans.length - 1) + ">";
    });

    // A link's opening tag goes the same way, and for the same reason one line further on: the
    // emphasis rules run after this one, and a `**` still looking for its partner takes the
    // first one it meets — which, once a link has been written, is the punctuation inside the
    // `href`. That is how `**對照頁更新了 → https://…9d**` came out as
    // `<a href="https://…9d</strong>">`: a bold line, and an address with a closing tag inside
    // it that no tap could follow. Only the tag is held back, not the label, so the emphasis
    // inside a written link's text still renders.
    var tags = [];
    function opening(html) { tags.push(html); return "<t" + (tags.length - 1) + ">"; }

    s = s
        // CommonMark autolinks, written links and bare ones in **one pass, alternating**, and the
        // order inside the pattern is the whole trick: at each position the delimited forms are
        // tried first, so they are consumed whole and the bare rule never sees the URL inside.
        // Two passes cannot do this — autolinking afterwards would match the `href="…"` the first
        // pass just produced and nest an anchor inside an anchor, and autolinking first would eat
        // the target out of every written link before the markdown rule could read it.
        //
        // Safe because `esc` has already run: what is matched here is escaped text, and what is
        // put in the attribute is the same escaped text, so a `"` in the source is `&quot;` and
        // cannot close the attribute.
        //
        // A bare address runs to whitespace, **except** for the full-width punctuation a Chinese
        // sentence is built from. A space is what ends a URL in English, and there is no space
        // after `，` — so `https://example.com/x，然後` was one address as far as this pattern
        // could tell, and the rest of the sentence went inside the link and turned blue. Trimming
        // the tail afterwards cannot undo that: by then the last character is `後`, not `，`.
        // Full-width brackets stay allowed, because a URL can genuinely contain them
        // (`…/wiki/中文（消歧義）`) and the balanced-closer rule below already handles the ones
        // that are really the sentence's.
        .replace(/&lt;(https?:\/\/[^\s<]+?)&gt;|\[([^\]\n]+)\]\(([^)\s]+)\)|(\bhttps?:\/\/[^\s<，。、；：！？]+)/g,
                 function (all, autolink, label, href, bare) {
            if (autolink) {
                return safeHref(autolink)
                    ? opening('<a href="' + autolink + '" target="_blank" rel="noopener noreferrer">')
                        + autolink + "</a>"
                    : all;
            }
            if (!bare) {
                return safeHref(href)
                    ? opening('<a href="' + href + '" target="_blank" rel="noopener noreferrer">')
                        + label + "</a>"
                    : localFileHref(href)
                        ? opening('<span class="local-ref" title="' + href + '">')
                            + label + "</span>"
                    : all;
            }
            // Punctuation that ends a sentence is not part of the address. A closing bracket is,
            // but only if the URL opened one — which is what people writing about Wikipedia keep
            // discovering. Anything trimmed goes back outside the link, where it was meant to be.
            //
            // `*`, `_` and `~` are in that list because a whole line is often bold — the address
            // included — and `…9d**` is then what the pattern sees. Trimming them puts the marker
            // back where it was written, outside the link, where the emphasis rule below can find
            // its partner and bold the sentence *and* the link. The cost is an address genuinely
            // ending in one of the three, which is not a thing people link to.
            //
            // The full-width brackets are in the closer list for the same reason the ASCII ones
            // are: `（見 https://example.com/a）` is a sentence in parentheses, not an address
            // ending in one.
            var closers = ")]}）」』》】〕", openers = "([{（「『《【〔";
            var enders = ".,;:!?'\u201d\u2019&*_~\u2026";
            var url = bare, tail = "";
            while (url.length) {
                var last = url.charAt(url.length - 1);
                if (closers.indexOf(last) >= 0) {
                    var open = openers.charAt(closers.indexOf(last));
                    // `>=`, not `>`: one opened and one closed is balanced, and the closer
                    // belongs to the address. `>` chopped the tail off `…/Foo_(bar)` — the
                    // exact case this check was written for.
                    if (url.split(open).length >= url.split(last).length) break;
                } else if (enders.indexOf(last) < 0) {
                    break;
                }
                // A quote at the end is punctuation around the address, not part of it — and by
                // this point `esc` has turned it into an entity, so it is six characters. The
                // apostrophe is the same story in five.
                if (/&quot;$/.test(url)) { tail = "&quot;" + tail; url = url.slice(0, -6); continue; }
                if (/&#39;$/.test(url)) { tail = "&#39;" + tail; url = url.slice(0, -5); continue; }
                // `&amp;` and friends end in `;`, and chopping the `;` off leaves `&amp` on the
                // screen — so an entity at the end is left whole.
                if (last === ";" && /&[a-z]+;$/i.test(url)) break;
                tail = last + tail;
                url = url.slice(0, -1);
            }
            if (!safeHref(url)) return all;
            return opening('<a href="' + url + '" target="_blank" rel="noopener noreferrer">')
                + url + "</a>" + tail;
        })
        // Lazy, and not "no marker inside": the bar recurses into what it finds between two
        // markers, so `**a *b* c**` is bold with an italic in it there. Refusing the whole thing
        // over the inner pair would put three pairs of asterisks back on the screen.
        .replace(/\*\*([^\n]+?)\*\*/g, "<strong>$1</strong>")
        .replace(/~~([^\n]+?)~~/g, "<del>$1</del>")
        // A single `*` or `_`, on the bar's terms: it has to open a word — start of the line, or
        // after a space or a bracket — and what it closes must not end in a space. `snake_case`
        // and `rm *.o *.a` are far more common in this text than emphasis is, and both of them
        // come through as themselves.
        .replace(/(^|[\s(])\*([^*\n]*[^\s*\n])\*/g, "$1<em>$2</em>")
        .replace(/(^|[\s(])_([^_\n]*[^\s_\n])_/g, "$1<em>$2</em>");

    s = s.replace(/<t(\d+)>/g, function (all, n) { return tags[n]; });
    return s.replace(/<(\d+)>/g, function (all, n) { return "<code>" + spans[n] + "</code>"; });
}

/** Only the schemes that cannot run anything. `javascript:` in a transcript is not a link. */
function safeHref(href) {
    return /^(https?:\/\/|mailto:)/i.test(href.replace(/&amp;/g, "&"));
}

/** A file on the Mac, not a route on this web app. Ordinary root-relative URLs stay unrecognised. */
function localFileHref(href) {
    var value = href.replace(/&amp;/g, "&");
    return /^\/(Users|private|tmp|var|Volumes|Applications|Library|opt|usr|home)\//.test(value)
        || /^file:\/\/\//i.test(value);
}
