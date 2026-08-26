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


def code_depths(text):
    """Brace depth for code bytes; comments and string contents are marked non-code."""
    depths = [-1] * len(text)
    depth = 0
    i = 0
    state = "code"
    quote = ""
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
                i += 1
            else:
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
        if char in "'\"`":
            state, quote = "string", char
            i += 1
            continue
        depths[i] = depth
        if char == "{":
            depth += 1
        elif char == "}" and depth:
            depth -= 1
        i += 1
    return depths


def top_level_ids(text):
    """Literal, unguarded getElementById calls at module/script top level."""
    depths = code_depths(text)
    pattern = re.compile(
        r"document\s*\.\s*getElementById\s*\(\s*(['\"])([^'\"]+)\1\s*\)"
    )
    return [(match.group(2), text.count("\n", 0, match.start()) + 1)
            for match in pattern.finditer(text) if depths[match.start()] == 0]


def main():
    try:
        html = INDEX.read_text()
    except OSError as error:
        print(f"check-web-ids: {error}")
        return 2

    defined = set(re.findall(r"\bid\s*=\s*['\"]([^'\"]+)['\"]", html))
    required = defaultdict(list)

    modules = sorted(JS_ROOT.rglob("*.js")) if JS_ROOT.exists() else []
    for path in modules:
        text = path.read_text()
        label = str(path.relative_to(ROOT))
        for element_id in registry_ids(text):
            required[element_id].append(label)
        for element_id, line in top_level_ids(text):
            required[element_id].append(f"{label}:{line}")

    # index.html still contains small inline boot scripts.  More importantly, this makes the
    # same check runnable against the pre-module history where all JavaScript lived in the page;
    # that is where f513ee5's missing tx-order ids can be seen red before green.
    parser = Scripts()
    parser.feed(html)
    inline = "\n".join(parser.parts)
    for element_id in registry_ids(inline):
        required[element_id].append("Resources/web/index.html (inline registry)")
    for element_id, line in top_level_ids(inline):
        required[element_id].append(f"Resources/web/index.html (inline script line {line})")

    missing = sorted(set(required) - defined)
    if missing:
        print("web element id contract does not agree:")
        for element_id in missing:
            print(f"  looked up but not defined: {element_id} ({', '.join(required[element_id])})")
        return 1

    print(f"web ids agree: {len(required)} looked up at load time, {len(defined)} defined in index.html")
    return 0


if __name__ == "__main__":
    sys.exit(main())
