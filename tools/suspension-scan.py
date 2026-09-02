#!/usr/bin/env python3
# Count suspension points owned directly by each function.
#
# A nested function is its own coroutine, so its awaits belong to it and not to its parent. That
# is exactly what the 28-section split relies on; counting them into the parent would report a
# split file as unchanged.
#
# Brace depth is counted WITHOUT special-casing multi-line string literals. That was tried and it
# made things worse: the triple-quote tracking mis-detected a boundary and ended
# runCloudAccountTests at line 1358 instead of 1567, under-reporting 143 awaits as 87. A validated
# simple counter beats an unvalidated clever one -- and the validation is the point: this script is
# checked against three independently known values before anything trusts it.
#
# The control set, kept here so the next person to change this scanner has one ready to re-run
# rather than having to trust that somebody once validated it:
#
#   Tests/CloudAccountTests.swift        runCloudAccountTests        143   (before the split)
#   Tests/CloudCommandLedgerTests.swift  runCloudCommandLedgerTests  131
#   Tests/CloudTransportTests.swift      runCloudTransportTests       61
#
# Those three came from independent per-file peak measurements on 2026-09-03, not from this
# script. Two earlier versions of it failed that control set: one special-cased multi-line string
# literals and reported 143 as 87; another counted nested functions into their parent and reported
# the split file as 171 instead of 15.
#
# The second failure is the dangerous one and is why the nesting rule is not an optimisation. A
# nested function is its own coroutine, so counting its awaits into the parent makes a correctly
# split file look untouched -- the guard would say "you did not fix it" at the exact moment
# somebody had. A guard that only ever over-reports is worse than no guard, because the first
# person it blocks raises the threshold to get past it.
import re, sys, pathlib

FUNC = re.compile(r"^(\s*)(?:private |fileprivate |public |internal |static |final )*func\s+([A-Za-z0-9_]+)")
AWAIT = re.compile(r"\bawait\b")

def scan(path):
    stack, out, depth = [], [], 0
    for ln in pathlib.Path(path).read_text().split("\n"):
        if ln.strip().startswith("//"):
            depth += ln.count("{") - ln.count("}")
            continue
        m = FUNC.match(ln)
        if m:
            stack.append([m.group(2), 0, depth])
        if stack:
            stack[-1][1] += len(AWAIT.findall(ln))
        depth += ln.count("{") - ln.count("}")
        while stack and depth <= stack[-1][2]:
            name, aw, _ = stack.pop()
            if stack:
                stack[-1][1] -= aw
            out.append((aw, name, path))
    while stack:
        name, aw, _ = stack.pop()
        out.append((aw, name, path))
    return out

rows = []
for p in sys.argv[1:]:
    rows.extend(scan(p))
rows.sort(reverse=True)
for aw, name, path in rows[:10]:
    print("%5d  %s  (%s)" % (aw, name, pathlib.Path(path).name))
