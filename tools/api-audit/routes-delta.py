#!/usr/bin/env python3
"""What the daemon answered between two readings of /v1/diagnostics. See README.md.

usage: python3 tools/api-audit/routes-delta.py <before.json> <after.json>

Each file is a whole `GET /v1/diagnostics` answer. Counters there are never
reset, so the calls in between are the second reading minus the first; the
latency columns are the second reading's own (the last calls each route kept).
"""
import json
import sys

if len(sys.argv) != 3 or sys.argv[1] in ("-h", "--help"):
    print(__doc__.strip(), file=sys.stderr)
    sys.exit(2)


def rows(path):
    with open(path) as fh:
        routes = json.load(fh).get("routes")
    if routes is None:
        print(f"routes-delta: {path} has no `routes` block (a daemon without the counter)", file=sys.stderr)
        sys.exit(1)
    return routes, {(r["method"], r["route"]): r for r in routes["routes"]}


before_block, before = rows(sys.argv[1])
after_block, after = rows(sys.argv[2])
if before_block["since"] != after_block["since"]:
    print("routes-delta: the daemon restarted between the readings; the counts are not comparable", file=sys.stderr)
    sys.exit(1)
CALLERS = ("device", "local", "cloud", "task", "anonymous")
out = []
for key, a in after.items():
    b = before.get(key, {})
    n = a["count"] - b.get("count", 0)
    opens = a.get("opens", 0) - b.get("opens", 0)
    if n == 0 and opens == 0:
        continue
    callers = {c: a["callers"][c] - b.get("callers", {}).get(c, 0) for c in CALLERS}
    lat = a.get("latency_ms") or {}
    out.append((n, key, opens, a.get("open", 0), callers, lat))
out.sort(key=lambda r: (-r[0], r[1]))
print(f"{'calls':>6} {'opens':>5} {'open':>4}  {'p50':>7} {'p90':>7} {'max':>8}  route  [callers]")
total = 0
for n, (method, route), opens, open_now, callers, lat in out:
    total += n
    who = " ".join(f"{c}={v}" for c, v in callers.items() if v)
    p = lambda k: f"{lat[k]:7.1f}" if k in lat else f"{'-':>7}"
    print(f"{n:6d} {opens:5d} {open_now:4d}  {p('p50')} {p('p90')} {lat.get('max', '-'):>8}  {method} {route}  [{who}]")
print(f"{total:6d} calls in all; overflow {after_block['overflow'] - before_block['overflow']}")
