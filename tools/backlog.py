#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Turn `docs/backlog.yaml` into a count anything can draw.

`--json` prints one line of JSON in the shape documented in `docs/project-status.md`, which is
what a status line or Clawdline's own footer reads. With no arguments it prints the list for a
person, which is the form you want when deciding what to do next.

**No PyYAML.** This parses the subset the file actually uses — comments, `key: value`, a list of
`- id:` blocks, and `>-` folded scalars — in about sixty lines. That is not stubbornness: this
repository's whole claim is that it installs with nothing, and a tool in it that needs `pip` makes
that claim false for anyone who wants to change the thing the tool describes.

**`lane` is computed here and cannot be written in the file.** See the header of
`docs/backlog.yaml` for why that is the entire design: a priority field that a person can set is
one that everything drifts to the top of.
"""

import html
import json
import os
import sys

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SOURCE = os.path.join(HERE, "docs", "backlog.yaml")

# severity x cost. Nothing else feeds it, and nothing in the file can override it.
LANES = {
    ("blocks", "hours"): "now",       ("blocks", "days"): "now",        ("blocks", "weeks"): "scheduled",
    ("degrades", "hours"): "now",     ("degrades", "days"): "scheduled", ("degrades", "weeks"): "waiting",
    ("noise", "hours"): "scheduled",  ("noise", "days"): "waiting",      ("noise", "weeks"): "drop",
    ("invisible", "hours"): "waiting", ("invisible", "days"): "drop",    ("invisible", "weeks"): "drop",
}
ORDER = ["now", "scheduled", "waiting", "drop"]


def parse(path):
    """The subset of YAML this file uses. Anything it does not understand is an error, loudly.

    Silently ignoring a line it cannot read would mean an item quietly vanishing from the count,
    and a backlog that undercounts is worse than no backlog — it reports progress that did not
    happen.
    """
    items, item, folding, top = [], None, None, {}
    for n, raw in enumerate(open(path, encoding="utf-8"), 1):
        line = raw.rstrip("\n")
        if folding is not None:
            # A folded scalar continues while the line is blank or indented past its key.
            if not line.strip() or line.startswith(" " * folding[2]):
                if line.strip():
                    item[folding[0]] = (item[folding[0]] + " " + line.strip()).strip()
                continue
            folding = None
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped == "items:":
            item = None          # anything after this belongs to an item, not to the file
            continue
        if stripped.startswith("- "):
            item = {}
            items.append(item)
            stripped = stripped[2:]
        if ":" not in stripped:
            raise SystemExit("backlog.yaml:%d: cannot read %r" % (n, line))
        key, _, value = stripped.partition(":")
        key, value = key.strip(), value.strip()
        if item is None:
            top[key] = value.strip('"').strip("'")
            continue
        if value in (">-", ">", "|-", "|"):
            item[key] = ""
            folding = (key, n, len(line) - len(line.lstrip()) + 2)
        else:
            item[key] = value.strip('"').strip("'")
    return top, items


def page(top, items, counts, out):
    """Write the page the count links to.

    Regenerated on every `--json`, which is to say every time the source changes, because the probe
    that asks for the count is the same thing that would notice a stale page. A backlog page that
    disagrees with the number beside it is worse than no page — the number is what people trust,
    and the page is where they go to find out what it means.
    """
    def esc(x):
        return html.escape(str(x or ""))

    rows = []
    for name in ORDER:
        group = [i for i in items if i["lane"] == name]
        if not group:
            continue
        rows.append('<h2>%s <span class="n">%d</span></h2>' % (esc(name), len(group)))
        for i in group:
            rows.append(
                '<article class="%s">'
                '<h3>%s <code>%s</code></h3>'
                '<p>%s</p>'
                '<dl><dt>evidence</dt><dd>%s</dd>'
                '<dt>reconsider when</dt><dd>%s</dd>'
                '<dt>opened</dt><dd>%s</dd>'
                '<dt>hurts</dt><dd>%s</dd>'
                '<dt>derived from</dt><dd>%s &times; %s</dd></dl>'
                '</article>' % (
                    esc(name), esc(i.get("title")), esc(i.get("id")), esc(i.get("problem")),
                    esc(i.get("evidence")), esc(i.get("trigger")), esc(i.get("opened")),
                    esc(i.get("who_hurts")), esc(i.get("severity")), esc(i.get("cost"))))

    doc = """<!doctype html><meta charset="utf-8"><title>Clawdline backlog</title>
<style>
:root{color-scheme:dark light}
body{max-width:52rem;margin:3rem auto;padding:0 1.5rem;
     font:16px/1.65 ui-serif,Georgia,serif;background:#14110f;color:#e8e2da}
h1{font-size:1.5rem;margin:0 0 .3rem}
.lede{color:#9a938b;margin:0 0 2.5rem}
h2{font:600 .8rem/1 ui-sans-serif,system-ui;letter-spacing:.14em;text-transform:uppercase;
   color:#9a938b;margin:2.5rem 0 .8rem;border-top:1px solid #2b2622;padding-top:1.2rem}
h2 .n{color:#5d564f}
article{margin:0 0 1.6rem;padding-left:.9rem;border-left:2px solid #2b2622}
article.now{border-left-color:#e06c4f}
h3{font-size:1rem;margin:0 0 .35rem;font-weight:600}
h3 code{font:500 .72rem/1 ui-monospace,SFMono-Regular,monospace;color:#7d766e;margin-left:.4rem}
p{margin:0 0 .5rem}
dl{display:grid;grid-template-columns:8.5rem 1fr;gap:.15rem .8rem;margin:0;
   font:.82rem/1.55 ui-sans-serif,system-ui;color:#9a938b}
dt{color:#5d564f}
dd{margin:0}
@media (prefers-color-scheme:light){
  body{background:#fbf9f6;color:#221d19}.lede,dl{color:#6b635b}h2 .n,dt{color:#948b82}
  h2,article{border-color:#e5ded6}h3 code{color:#8a8179}}
</style>
<h1>Backlog</h1>
<p class="lede">%d items &middot; %s. <strong>Lane is derived from severity &times; cost</strong> and
cannot be set by hand &mdash; see the header of <code>docs/backlog.yaml</code>.</p>
%s
""" % (len(items), " &middot; ".join("%s %d" % (n, counts[n]) for n in ORDER), "\n".join(rows))

    os.makedirs(os.path.dirname(out), exist_ok=True)
    with open(out, "w", encoding="utf-8") as f:
        f.write(doc)


def lane(item):
    key = (item.get("severity"), item.get("cost"))
    if key not in LANES:
        raise SystemExit("%s: severity/cost is %r, which is not a pair I know" % (item.get("id"), key))
    return LANES[key]


def main():
    top, items = parse(SOURCE)
    for it in items:
        it["lane"] = lane(it)
    counts = {name: sum(1 for i in items if i["lane"] == name) for name in ORDER}

    # Where the count links to. A `http(s)` value is somebody else's page and is reported
    # untouched; anything else is a path in this repository that we generate. The distinction
    # matters because a project whose backlog lives on the web should not have a local file
    # written for it, and a project like this one has nowhere to host a page and does not need to.
    where = top.get("artifact", "")
    artifact = ""
    if where.startswith("http://") or where.startswith("https://"):
        artifact = where
    elif where:
        artifact = os.path.join(HERE, where)
        page(top, items, counts, artifact)

    if "--json" in sys.argv:
        print(json.dumps({
            "source": SOURCE,
            "total": len(items),
            "lanes": counts,
            "artifact": artifact,
        }, ensure_ascii=False))
        return 0

    width = max((len(i.get("id", "")) for i in items), default=0)
    for name in ORDER:
        rows = [i for i in items if i["lane"] == name]
        if not rows:
            continue
        print("\n%s (%d)" % (name, len(rows)))
        for i in rows:
            print("  %-*s  %s" % (width, i.get("id", "?"), i.get("title", "")))
    print("\n%d items · %s" % (len(items), " · ".join("%s %d" % (n, counts[n]) for n in ORDER)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
