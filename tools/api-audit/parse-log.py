#!/usr/bin/env python3
"""Count what the daemon's own log says about Cloud traffic. See README.md.

usage: python3 tools/api-audit/parse-log.py <log-dir> [<out.json>]

<log-dir> holds `daemon.log` and its rotated `daemon.2*.log` files. The summary
is printed and written to <out.json> (default `logstats.json` in the current
directory). Ids, panes and numbers are taken out of the line shapes it counts.
"""
import collections
import datetime as dt
import glob
import json
import os
import re
import sys

if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
    print(__doc__.strip(), file=sys.stderr)
    sys.exit(2)
logdir = sys.argv[1]
outpath = sys.argv[2] if len(sys.argv) > 2 else "logstats.json"
files = sorted(glob.glob(os.path.join(logdir, "daemon.2*.log")))
if os.path.exists(os.path.join(logdir, "daemon.log")):
    files.append(os.path.join(logdir, "daemon.log"))
if not files:
    print(f"parse-log: no daemon.log in {logdir}", file=sys.stderr)
    sys.exit(1)

TS = re.compile(r'^(\d{4}/\d\d/\d\d \d\d:\d\d:\d\d) (.*)$')
ack = re.compile(r'cloud ack seq=\d+ ch=([a-z]+)/[^/ ]+(?:/(\S+))? status=(\S+) fanout=(\d+)')
rd = re.compile(r'cloud: read answered: operation=(\S+) status=(\d+) code=(\S+) ms=(\d+) sender=(\S+)')
cmd = re.compile(r'cloud: command answered: operation=(\S+) session=\S+ status=(\d+) code=(\S+)')
osa = re.compile(r'iterm: osascript failed: kind=(\S+) .*elapsed=([\d.]+)s reason=(\S+)')
inv = re.compile(r'cloud: inventory published: complete=(\S+) .*incomplete_sources=\[([^\]]*)\]')


def shape(s):
    s = re.sub(r'[0-9a-f]{8}-[0-9a-f-]{27,}', 'ID', s)
    s = re.sub(r'%\d+|ttys\d+', 'PANE', s)
    s = re.sub(r'\d+(\.\d+)?', 'N', s)
    return s[:110]


def pct(v, q):
    v = sorted(v)
    return v[min(len(v) - 1, int(q * len(v)))] if v else None


first = last = None
acks = collections.Counter()
ackStatus = collections.Counter()
ackHour = collections.defaultdict(collections.Counter)
ackSess = collections.Counter()
reads = collections.defaultdict(list)
readCodes = collections.Counter()
cmds = collections.Counter()
osas = collections.Counter()
osaHour = collections.Counter()
osaEl = []
invc = collections.Counter()
shapes = collections.Counter()
readMin = collections.Counter()
osaMin = set()
fan = collections.Counter()
for f in files:
    for line in open(f, errors='replace'):
        m = TS.match(line.rstrip('\n'))
        if not m:
            continue
        t, body = m.groups()
        if first is None or t < first:
            first = t
        if last is None or t > last:
            last = t
        hour = t[:13].replace('/', '-')
        shapes[shape(body)] += 1
        a = ack.search(body)
        if a:
            p, sess, st, fo = a.groups()
            acks[p] += 1
            ackStatus[st] += 1
            ackHour[hour][p] += 1
            fan[fo] += 1
            if sess:
                kind = 'inventory' if 'inventory' in sess else ('machine' if 'machine' in sess else 'session')
                ackSess[(p, kind)] += 1
            continue
        r = rd.search(body)
        if r:
            op, stt, code, ms, _sender = r.groups()
            reads[op].append(int(ms))
            readCodes[(op, stt, code)] += 1
            readMin[t[:16]] += 1
            continue
        c = cmd.search(body)
        if c:
            cmds[(c.group(1), c.group(3))] += 1
            continue
        o = osa.search(body)
        if o:
            osas[(o.group(1), o.group(3))] += 1
            osaHour[hour] += 1
            osaEl.append(float(o.group(2)))
            osaMin.add(t[:16])
            continue
        i = inv.search(body)
        if i:
            invc[(i.group(1), i.group(2) or '-')] += 1

if first is None:
    print("parse-log: no timestamped line in the logs", file=sys.stderr)
    sys.exit(1)
t0 = dt.datetime.strptime(first, '%Y/%m/%d %H:%M:%S')
t1 = dt.datetime.strptime(last, '%Y/%m/%d %H:%M:%S')
out = dict(
    first=first, last=last, hours=round((t1 - t0).total_seconds() / 3600, 1),
    acks=dict(acks), ackStatus=dict(ackStatus), fanout=dict(fan),
    ackSess={f'{k[0]}:{k[1]}': v for k, v in ackSess.items()},
    ackHour={h: dict(c) for h, c in sorted(ackHour.items())},
    reads={op: dict(n=len(v), p50=pct(v, .5), p90=pct(v, .9), max=max(v)) for op, v in reads.items()},
    readCodes=[[*k, v] for k, v in readCodes.most_common()],
    slowReadMinutes=len(readMin),
    slowReadMinutesWithOsaFail=sum(1 for m in readMin if m in osaMin),
    cmds=[[*k, v] for k, v in cmds.most_common()],
    osa=[[*k, v] for k, v in osas.most_common()], osaHour=dict(sorted(osaHour.items())), osaN=len(osaEl),
    inventory=[[*k, v] for k, v in invc.most_common()],
    shapes=shapes.most_common(40))
with open(outpath, 'w') as fh:
    json.dump(out, fh, ensure_ascii=False, indent=1)
keys = ['first', 'last', 'hours', 'acks', 'ackStatus', 'fanout', 'ackSess', 'reads', 'readCodes',
        'slowReadMinutes', 'slowReadMinutesWithOsaFail', 'cmds', 'osa', 'osaN', 'inventory']
print(json.dumps({k: out[k] for k in keys}, ensure_ascii=False, indent=0))
for s, n in out['shapes']:
    print(n, s)
