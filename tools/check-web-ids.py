#!/usr/bin/env python3
"""Check load-time element lookups against the ids in the web page."""
import re
import sys
from collections import defaultdict
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
WEB = ROOT / "Resources" / "web"
INDEX = WEB / "index.html"
JS_ROOT = WEB / "app" / "js"

# An `id="…"` written in markup, wherever that markup is authored. `index.html` is not
# the only place the page defines an id: a module that builds its own sheet, writes it
# into `innerHTML` and appends it defines those ids as surely as the document does —
# `input/user-messages.js` and `view/transcript.js` both do exactly that. Reading only
# `index.html` called that a broken contract and went red on correct code, which is the
# tax that gets a check deleted rather than fixed.
MARKUP_ID = re.compile(r"\bid\s*=\s*['\"]([^'\"]+)['\"]")

# Nearly every id this check knows about comes out of one literal array, matched by one regular
# expression in registry_ids().  Rewriting its callback as an arrow function, or putting a
# grouping comment inside the array, drops the match to nothing — and the script would then print
# "web ids agree: 4 looked up at load time" and exit 0 with an id genuinely missing from the page.
# So the count is a tripwire, not a coverage target: the smallest registry in this repository's
# history is the 27 ids of c2730db (2026-08-18) and it has only ever grown, so no real tree has
# ever come near 24, while every way of breaking the pattern that has been tried lands on 0.
# If the registry ever legitimately shrinks past this, lower it in the commit that shrinks it.
REGISTRY_FLOOR = 24

# A `/` after one of these is a regular expression; after anything else it is division.  This is
# what a JavaScript lexer does, and it is the whole of the ambiguity: the same character opens a
# literal or divides two numbers depending on the token in front of it.
REGEX_AFTER = "(,;:=!&|?+-*%~^<>[{}"
REGEX_AFTER_WORD = {"return", "typeof", "instanceof", "in", "of", "new", "delete",
                    "void", "throw", "case", "do", "else", "yield", "await"}


def fail(message):
    print(f"check-web-ids: {message}")
    sys.exit(2)


class Scripts(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=False)
        self.in_script = False
        self.parts = []

    def handle_starttag(self, tag, attrs):
        if tag.lower() == "script" and not dict(attrs).get("src"):
            self.in_script = True

    def handle_endtag(self, tag):
        if tag.lower() == "script":
            self.in_script = False

    def handle_data(self, data):
        if self.in_script:
            self.parts.append(data)


def registry_ids(text):
    """The literal array feeding els[id] = getElementById(id), before and after the JS split."""
    pattern = re.compile(
        r"\[(?P<body>(?:\s*['\"][^'\"]+['\"]\s*,?)+)\]"
        r"\s*\.forEach\s*\(\s*function\s*\(\s*id\s*\)\s*\{\s*"
        r"els\s*\[\s*id\s*\]\s*=\s*document\.getElementById\s*\(\s*id\s*\)",
        re.DOTALL,
    )
    out = set()
    for match in pattern.finditer(text):
        out.update(re.findall(r"['\"]([^'\"]+)['\"]", match.group("body")))
    return out


def starts_regex(text, last, last_index):
    """Is a `/` following text[last_index] the start of a literal rather than a division?"""
    if not last:
        return True
    if last in "+-" and last_index and text[last_index - 1] == last:
        return False  # `i++ / 2` divides; every other `+` or `-` in front of a `/` cannot.
    if last in REGEX_AFTER:
        return True
    if last.isalnum() or last in "_$":
        start = last_index
        while start and (text[start - 1].isalnum() or text[start - 1] in "_$"):
            start -= 1
        if start and text[start - 1] == ".":
            return False  # `iter.return / 2` is a property read, so the slash divides.
        return text[start:last_index + 1] in REGEX_AFTER_WORD
    return False


def code_depths(text):
    """Brace depth for code bytes; comments, strings and regex bodies are marked non-code.

    Returns the depths and what the scanner was still holding at the end.  A file it followed
    all the way through ends at depth 0 in code; anything else means it lost its place partway,
    and every depth it recorded after that point is a guess — see scan(), which refuses them.
    """
    depths = [-1] * len(text)
    depth = 0
    i = 0
    state = "code"
    quote = ""
    last = ""
    last_index = 0
    while i < len(text):
        char = text[i]
        nxt = text[i + 1] if i + 1 < len(text) else ""
        if state == "line":
            if char == "\n":
                state = "code"
            i += 1
            continue
        if state == "block":
            if char == "*" and nxt == "/":
                state = "code"
                i += 2
            else:
                i += 1
            continue
        if state == "string":
            if char == "\\":
                i += 2
            elif char == quote:
                state = "code"
                last, last_index = char, i
                i += 1
            else:
                i += 1
            continue
        if state in ("regex", "class"):
            # A regex literal cannot span a line, so a newline here means the `/` that opened it
            # was really a division.  Closing it at the break keeps that mistake one line wide
            # instead of swallowing the rest of the file, and leaves the depth check to notice.
            if char == "\\":
                i += 2
                continue
            if char == "\n":
                state = "code"
            elif state == "class":
                if char == "]":
                    state = "regex"
            elif char == "[":
                # `/[/]/` is legal: inside a character class the delimiter is an ordinary byte.
                state = "class"
            elif char == "/":
                state = "code"
                last, last_index = char, i
            i += 1
            continue
        if char == "/" and nxt == "/":
            state = "line"
            i += 2
            continue
        if char == "/" and nxt == "*":
            state = "block"
            i += 2
            continue
        if char == "/" and starts_regex(text, last, last_index):
            # Without this, `.replace(/"/g, …)` opens a string on the quote inside the pattern,
            # `/^https?:\/\//i` reads as a line comment, and a backtick in a pattern turns the
            # rest of the file into a template literal.  All three are in this tree.
            state = "regex"
            i += 1
            continue
        if char in "'\"`":
            state, quote = "string", char
            i += 1
            continue
        depths[i] = depth
        if char == "{":
            depth += 1
        elif char == "}" and depth:
            depth -= 1
        if not char.isspace():
            last, last_index = char, i
        i += 1
    return depths, depth, state


def scan(label, text):
    """code_depths for one source, refusing one whose depths cannot be trusted.

    Three files in this tree used to end at a non-zero depth because of regex literals the
    scanner did not know, which quietly put every lookup in them outside the guarantee: a
    top-level getElementById in core/esc.js was scored at depth 1 and never checked at all.
    A blank answer that reads as agreement is the failure this whole check exists to prevent,
    so an unbalanced file is loud.
    """
    depths, depth, state = code_depths(text)
    if depth or state not in ("code", "line"):
        fail(f"{label}: the brace scanner ended at depth {depth} in {state} rather than depth 0 "
             f"in code — it lost its place, so the depths it recorded there mean nothing")
    return depths


def top_level_ids(text, depths):
    """Literal, unguarded getElementById calls at module/script top level."""
    pattern = re.compile(
        r"document\s*\.\s*getElementById\s*\(\s*(['\"])([^'\"]+)\1\s*\)"
    )
    return [(match.group(2), text.count("\n", 0, match.start()) + 1, match.start())
            for match in pattern.finditer(text) if depths[match.start()] == 0]


def main():
    try:
        html = INDEX.read_text()
    except OSError as error:
        fail(str(error))

    defined = set(MARKUP_ID.findall(html))
    required = defaultdict(list)
    registry = set()
    # Where each module first writes a given `id="…"`, by character offset. Two limits, and the
    # check is only honest with both. **Same file**: pooling these was the first version of this
    # fix, and a stale comment in one module naming `id="toast"` then vouched for a lookup of
    # `toast` in a different module whose element had genuinely been deleted from the page.
    # **Before the lookup**: markup written after the line that reads it answers nothing, because
    # `getElementById` runs when the module is evaluated, top to bottom.
    #
    # It is a heuristic and stays one: this cannot tell markup a module inserts on load from
    # markup it only inserts when something is pressed, and a same-file lookup placed after a
    # deferred `appendChild` still passes. That limit is written down rather than papered over.
    authored = {}

    modules = sorted(JS_ROOT.rglob("*.js")) if JS_ROOT.exists() else []
    for path in modules:
        try:
            text = path.read_text()
        except OSError as error:
            fail(str(error))
        label = str(path.relative_to(ROOT))
        first = {}
        for match in MARKUP_ID.finditer(text):
            first.setdefault(match.group(1), match.start())
        authored[label] = first
        depths = scan(label, text)
        for element_id in registry_ids(text):
            # The els registry is the index.html element table by definition; nothing a module
            # writes for itself answers it.
            required[element_id].append((label, None, None))
            registry.add(element_id)
        for element_id, line, start in top_level_ids(text, depths):
            required[element_id].append((f"{label}:{line}", label, start))

    # index.html still contains small inline boot scripts.  More importantly, this makes the
    # same check runnable against the pre-module history where all JavaScript lived in the page;
    # that is where f513ee5's missing tx-order ids can be seen red before green.
    parser = Scripts()
    parser.feed(html)
    inline = "\n".join(parser.parts)
    inline_depths = scan("Resources/web/index.html (inline scripts)", inline)
    for element_id in registry_ids(inline):
        required[element_id].append(("Resources/web/index.html (inline registry)", None, None))
        registry.add(element_id)
    for element_id, line, _ in top_level_ids(inline, inline_depths):
        required[element_id].append(
            (f"Resources/web/index.html (inline script line {line})", None, None))

    # Before comparing, say whether there was anything to compare.  Everything above can come
    # back empty — a renamed directory, a rewritten callback — and an empty answer here would
    # otherwise print agreement and exit 0.
    if len(registry) < REGISTRY_FLOOR:
        fail(f"found {len(registry)} ids in the els registry, under the floor of {REGISTRY_FLOOR}: "
             f"the literal array feeding els[id] = document.getElementById(id) is not in "
             f"{JS_ROOT.relative_to(ROOT)} or in the inline scripts of "
             f"{INDEX.relative_to(ROOT)} in the shape registry_ids() looks for")

    # A lookup is answered by the page, or by the very file that makes it — never by a third one.
    missing = []
    for element_id in sorted(required):
        if element_id in defined:
            continue
        unresolved = []
        for site, owner, start in required[element_id]:
            written = authored.get(owner, {}).get(element_id) if owner else None
            if written is None or start is None or written >= start:
                unresolved.append(site)
        if unresolved:
            missing.append((element_id, unresolved))
    if missing:
        print("web element id contract does not agree:")
        for element_id, sites in missing:
            print(f"  looked up but not defined: {element_id} ({', '.join(sites)})")
        return 1

    print(f"web ids agree: {len(required)} looked up at load time, "
          f"{len(defined)} defined in index.html")
    return 0


if __name__ == "__main__":
    sys.exit(main())
