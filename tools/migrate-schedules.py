#!/usr/bin/env python3
"""Move the Swift app's schedule files into this daemon, and prove they arrived.

    tools/migrate-schedules.py import <copy-dir> <port> <state-dir>
    tools/migrate-schedules.py verify <copy-dir> <port> <state-dir>

<copy-dir> is a copy of the Swift app's `schedules/*.json`, never the Swift
app's own directory: a path that is, or resolves into, ~/.config/clawdline is
refused, and so is a file in the copy that is a symbolic or hard link. Nothing
is ever written there — this script only reads it.

<port> and <state-dir> name the new daemon and the CLAWDLINE_NEXT_DIR it was
started with. Neither has a default, because a migration that guesses its target
is one that can land on the wrong daemon. Before the token leaves this process
the port must answer /v1/health as `clawdline-go`; the token itself is read from
<state-dir>/orchestrator-token, and a daemon that refuses it keeps its state
somewhere else, so the script stops there.

`import` sends every *.json file whole, as the bytes on disk, and prints what
the daemon answered for each one and when it next fires. The daemon refuses the
whole import unless its config.json has `"schedule_imports_enabled": true`; that
refusal is printed in the daemon's own words.

`verify` changes nothing and compares three independent ways
(docs/schedules.md, "Migration"):

  bytes      sha256 of the source file = sha256 of the stored file, as
             GET /v1/orchestrator/schedule-exports returns it
  parsed     what the daemon's own parser read (title, enabled, when, close_tab,
             catch_up_hours, notify_on_failure and the whole task template) =
             the source file, with the Swift app's defaults for absent fields
  next_fire  the daemon's next firing = the next firing computed here from the
             source `when`, in this machine's local time

Schedule titles are never printed, so the output can be pasted into a report.

Three answers, as the other tools here: 0 is "every file is in and the same",
1 is "something differs or needs a person", 2 is "this could not be done or
checked" — a refusal is never reported as the green one.

Only the standard library is used.
"""

import argparse
import datetime
import decimal
import hashlib
import json
import os
import pwd
import stat
import sys
import time
import urllib.error
import urllib.request

SERVED_BY = "clawdline-go"
TOKEN_FILE = "orchestrator-token"
TOKEN_HEADER = "X-Clawdline-Orchestrator"
TOKEN_LIMIT = 512
# The daemon's body limit for /v1/orchestrator/schedule-imports.
IMPORT_LIMIT = 4 << 20
DAYS = ("sun", "mon", "tue", "wed", "thu", "fri", "sat")
# What the parser gives a file that leaves the field out (internal/domain/schedule).
DEFAULTS = {"close_tab": "on_success", "catch_up_hours": 6, "notify_on_failure": True}
PARSED_FIELDS = ("title", "enabled", "when", "close_tab", "catch_up_hours", "notify_on_failure", "task")
GOOD_IMPORT_STATES = ("imported", "unchanged")


class Refused(Exception):
    """Something this script will not do, or could not find out."""


# ---------------------------------------------------------------- the paths


def swift_dirs():
    """The Swift app's state directory, for every home this process could mean."""
    homes = {os.path.expanduser("~")}
    try:
        homes.add(pwd.getpwuid(os.getuid()).pw_dir)
    except KeyError:
        pass
    return sorted(os.path.join(h, ".config", "clawdline") for h in homes if h)


def spelled_inside(path, root):
    """Whether `path`, as spelled, is `root` or below it. Case is folded
    because the default macOS volume does not tell `.Config` from `.config`."""
    p = os.path.normcase(os.path.abspath(path)).lower()
    r = os.path.normcase(os.path.abspath(root)).lower()
    return p == r or p.startswith(r.rstrip(os.sep) + os.sep)


def same_file_ancestor(path, roots):
    """The first of `roots` that `path`, once resolved, is or lies inside of —
    decided by device and inode, so no spelling of the path can step around it."""
    ids = {}
    for root in roots:
        try:
            st = os.stat(root)
        except OSError:
            continue
        ids[(st.st_dev, st.st_ino)] = root
    if not ids:
        return None
    p = os.path.realpath(path)
    while True:
        try:
            st = os.stat(p)
        except OSError:
            st = None
        if st is not None and (st.st_dev, st.st_ino) in ids:
            return ids[(st.st_dev, st.st_ino)]
        parent = os.path.dirname(p)
        if parent == p:
            return None
        p = parent


def refuse_swift_dir(path, what, advice):
    for root in swift_dirs():
        if spelled_inside(path, root):
            raise Refused("%s %s is inside %s, the Swift app's own directory. %s" % (what, path, root, advice))
    hit = same_file_ancestor(path, swift_dirs())
    if hit:
        raise Refused("%s %s resolves into %s, the Swift app's own directory. %s" % (what, path, hit, advice))


COPY_ADVICE = ("Copy the files first (cp -p ~/.config/clawdline/schedules/*.json <copy-dir>/) "
               "and give the copy.")


def read_source(directory):
    """Every *.json in the copy, as (name, bytes, text), and the names left out."""
    refuse_swift_dir(directory, "The source", COPY_ADVICE)
    if not os.path.isdir(directory):
        raise Refused("The source %s is not a directory." % directory)
    files, ignored = [], []
    with os.scandir(directory) as entries:
        names = sorted(e.name for e in entries)
    for name in names:
        if not name.endswith(".json"):
            ignored.append(name)
            continue
        path = os.path.join(directory, name)
        st = os.lstat(path)
        if stat.S_ISLNK(st.st_mode):
            raise Refused("%s is a symbolic link; copy the file itself. %s" % (path, COPY_ADVICE))
        if not stat.S_ISREG(st.st_mode):
            raise Refused("%s is not a plain file." % path)
        if st.st_nlink > 1:
            raise Refused("%s is a hard link (%d names share it), which is not a copy. %s"
                          % (path, st.st_nlink, COPY_ADVICE))
        with open(path, "rb") as f:
            raw = f.read()
        try:
            text = raw.decode("utf-8")
        except UnicodeDecodeError:
            # JSON strings carry text; these bytes would arrive changed, and a
            # changed file is not a migrated one. Nothing has been sent yet.
            raise Refused("%s is not UTF-8 text, so it cannot be carried byte for byte. "
                          "Nothing was sent; move it out of the copy or fix it." % path)
        files.append((name, raw, text))
    if not files:
        raise Refused("There is no *.json file in %s." % directory)
    return files, ignored


def read_token(state):
    refuse_swift_dir(state, "The state directory", "Give the new daemon's CLAWDLINE_NEXT_DIR.")
    path = os.path.join(state, TOKEN_FILE)
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        raise Refused("There is no %s — has a daemon started with this state directory? "
                      "(CLAWDLINE_NEXT_DIR chooses it)" % path)
    if not stat.S_ISREG(st.st_mode):
        raise Refused("%s is not a plain file; it was not read." % path)
    with open(path, "rb") as f:
        data = f.read(TOKEN_LIMIT + 1)
    token = data.decode("utf-8", "replace").strip()
    if len(data) > TOKEN_LIMIT or not token or any(c.isspace() for c in token):
        raise Refused("%s does not hold a usable token." % path)
    return token


# ----------------------------------------------------------------- the wire


class NoRedirect(urllib.request.HTTPRedirectHandler):
    """A redirect would carry the token somewhere nobody named."""

    def redirect_request(self, *args, **kwargs):
        return None


# Proxies are left out as well: the environment's http_proxy has no business
# with a loopback request that carries a credential.
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())


class Daemon:
    def __init__(self, port, state):
        self.port = port
        self.state = state
        self.token = None

    def call(self, method, path, body=None, authed=True):
        url = "http://127.0.0.1:%d%s" % (self.port, path)
        req = urllib.request.Request(url, data=body, method=method)
        if authed:
            req.add_header(TOKEN_HEADER, self.token)
        if body is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with OPENER.open(req, timeout=60) as resp:
                return resp.status, resp.read()
        except urllib.error.HTTPError as e:
            return e.code, e.read()
        except (urllib.error.URLError, OSError) as e:
            raise Refused("Nothing answered on 127.0.0.1:%d (%s)." % (self.port, getattr(e, "reason", e)))

    def get_json(self, path):
        status, raw = self.call("GET", path)
        if status != 200:
            raise Refused("GET %s was refused: %s" % (path, refusal(status, raw)))
        return decode(raw, path)

    def identify(self):
        """Prove the port is the new daemon of <state-dir>, and return what it
        holds (the export). The token is not sent before the first half."""
        status, raw = self.call("GET", "/v1/health", authed=False)
        served_by = None
        if status == 200:
            try:
                served_by = json.loads(raw.decode("utf-8")).get("served_by")
            except (ValueError, AttributeError):
                served_by = None
        if served_by != SERVED_BY:
            raise Refused("127.0.0.1:%d is not the new daemon: /v1/health answered %d with served_by=%r, "
                          "not %r. (The Swift app, usually on 7717, takes no imports.) Give the port of the "
                          "daemon started with CLAWDLINE_NEXT_DIR=%s."
                          % (self.port, status, served_by, SERVED_BY, self.state))
        self.token = read_token(self.state)
        status, raw = self.call("GET", "/v1/orchestrator/schedule-exports")
        # The gate answers 401 for a token it does not hold, the route 403.
        if status in (401, 403):
            raise Refused("The daemon on 127.0.0.1:%d refused %s (%s). It keeps its state in another "
                          "directory: give the CLAWDLINE_NEXT_DIR it was started with, or the port of the "
                          "daemon that uses %s."
                          % (self.port, os.path.join(self.state, TOKEN_FILE), refusal(status, raw), self.state))
        if status != 200:
            raise Refused("GET /v1/orchestrator/schedule-exports was refused: %s" % refusal(status, raw))
        files = decode(raw, "the export").get("files")
        if not isinstance(files, dict):
            raise Refused("The export did not carry a files object.")
        return files


def decode(raw, what):
    try:
        value = json.loads(raw.decode("utf-8"), parse_float=decimal.Decimal)
    except ValueError:
        raise Refused("%s is not JSON." % what)
    if not isinstance(value, dict):
        raise Refused("%s is not a JSON object." % what)
    return value


def refusal(status, raw):
    """The daemon's refusal in its own words: `<status> <code>: <message>`."""
    try:
        err = json.loads(raw.decode("utf-8")).get("error")
    except (ValueError, AttributeError):
        err = None
    if isinstance(err, dict) and err.get("message"):
        return "%d %s: %s" % (status, err.get("code", ""), err["message"])
    text = raw.decode("utf-8", "replace").strip()
    return "%d %s" % (status, text[:500] or "(no body)")


# ------------------------------------------------------- the local reading


def parse_when(w):
    """(hour, minute, on, days) from a file's `when`, or None where the
    grammar is not met. `on` is a date or None; `days` is "daily" or a set of
    weekday numbers, Sunday = 0."""
    if not isinstance(w, dict) or set(w) not in ({"at", "days"}, {"at", "on"}):
        return None
    at = w.get("at")
    if not isinstance(at, str) or len(at) != 5 or at[2] != ":" or not (at[:2] + at[3:]).isdigit() \
            or not at.isascii():
        return None
    hour, minute = int(at[:2]), int(at[3:])
    if hour > 23 or minute > 59:
        return None
    if "on" in w:
        on = w["on"]
        if not isinstance(on, str) or len(on) != 10 or on[4] != "-" or on[7] != "-" \
                or not (on[:4] + on[5:7] + on[8:]).isdigit() or not on.isascii():
            return None
        try:
            day = datetime.date(int(on[:4]), int(on[5:7]), int(on[8:]))
        except ValueError:
            return None
        if day.year < 1970:
            return None
        return hour, minute, day, None
    days = w.get("days")
    if days == "daily":
        return hour, minute, None, "daily"
    if not isinstance(days, list) or not days or any(d not in DAYS for d in days) \
            or len(set(days)) != len(days):
        return None
    return hour, minute, None, {DAYS.index(d) for d in days}


def local_instant(day, hour, minute):
    return int(time.mktime((day.year, day.month, day.day, hour, minute, 0, 0, 0, -1)))


def next_fire(parsed, now):
    """The first firing after `now`, in local time, or None — the same walk the
    daemon makes (When.NextFire): today and the seven days after it."""
    hour, minute, on, days = parsed
    if on is not None:
        t = local_instant(on, hour, minute)
        return t if t > now else None
    today = datetime.date.fromtimestamp(now)
    for ahead in range(8):
        t = local_instant(today + datetime.timedelta(days=ahead), hour, minute)
        if t <= now:
            continue
        weekday = (datetime.date.fromtimestamp(t).weekday() + 1) % 7
        if days == "daily" or weekday in days:
            return t
    return None


def normal_when(w):
    """`when` with `days` as a set: the parser keeps which days, not their order."""
    if isinstance(w, dict) and isinstance(w.get("days"), list):
        out = dict(w)
        out["days"] = sorted(w["days"], key=lambda d: DAYS.index(d) if d in DAYS else len(DAYS))
        return out
    return w


def strict_equal(a, b):
    """JSON equality that does not let true stand for 1."""
    if isinstance(a, bool) or isinstance(b, bool):
        return isinstance(a, bool) and isinstance(b, bool) and a == b
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(strict_equal(a[k], b[k]) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(strict_equal(x, y) for x, y in zip(a, b))
    numbers = (int, decimal.Decimal)
    if isinstance(a, numbers) and isinstance(b, numbers):
        return a == b
    return type(a) is type(b) and a == b


def differing_fields(source, stored):
    """The names of the parsed fields that differ; `task.<key>` inside the template."""
    out = []
    for field in PARSED_FIELDS:
        want = source.get(field, DEFAULTS.get(field))
        have = stored.get(field)
        if field == "when":
            want, have = normal_when(want), normal_when(have)
        if strict_equal(want, have):
            continue
        if field == "task" and isinstance(want, dict) and isinstance(have, dict):
            keys = sorted(set(want) | set(have))
            out.extend("task." + k for k in keys if k not in want or k not in have
                       or not strict_equal(want[k], have[k]))
        else:
            out.append(field)
    return out


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def when_text(unix):
    if unix is None:
        return "never"
    return time.strftime("%Y-%m-%d %H:%M %a", time.localtime(unix))


def source_object(text):
    try:
        obj = json.loads(text, parse_float=decimal.Decimal)
    except ValueError:
        return None
    return obj if isinstance(obj, dict) else None


# ---------------------------------------------------------------- import


def run_import(args):
    files, ignored = read_source(args.source)
    daemon = Daemon(args.port, args.state)
    daemon.identify()
    body = json.dumps({"files": {name: text for name, _, text in files}}, ensure_ascii=False).encode("utf-8")
    if len(body) > IMPORT_LIMIT:
        raise Refused("These %d files are %d bytes together, more than the %d one import takes. "
                      "Split the copy into two directories and import each." % (len(files), len(body), IMPORT_LIMIT))
    status, raw = daemon.call("POST", "/v1/orchestrator/schedule-imports", body=body)
    if status != 200:
        raise Refused("The daemon refused the import: %s" % refusal(status, raw))
    results = decode(raw, "the import answer").get("results")
    if not isinstance(results, list):
        raise Refused("The import answer did not carry results.")
    by_file = {r.get("file"): r for r in results if isinstance(r, dict)}

    print("import  %s -> 127.0.0.1:%d (%s)" % (args.source, args.port, args.state))
    if ignored:
        print("ignored %d entr%s not named *.json: %s"
              % (len(ignored), "y" if len(ignored) == 1 else "ies", ", ".join(ignored)))
    width = max(len(name) for name, _, _ in files)
    counts = {}
    clean = True
    for name, source_raw, _ in files:
        row = by_file.get(name)
        if row is None:
            state, line = "unanswered", "the daemon said nothing about this file"
        else:
            state, line = str(row.get("state", "?")), ""
            if "sha256" in row and row["sha256"] != sha(source_raw):
                state = "altered"
                line = "the daemon received different bytes (sha256 %s, sent %s)" % (
                    str(row["sha256"])[:12], sha(source_raw)[:12])
            elif state == "exists":
                line = "a different file with this id is already stored; it was left as it is"
            elif row.get("error"):
                line = str(row["error"])
            elif row.get("next_fire"):
                line = "next " + when_text(row["next_fire"])
                if row.get("enabled") is False:
                    line += " (disabled)"
        counts[state] = counts.get(state, 0) + 1
        clean = clean and state in GOOD_IMPORT_STATES
        print("  %-*s  %-10s  %s" % (width, name, state, line))
    print("%d file%s: %s" % (len(files), "" if len(files) == 1 else "s",
                             ", ".join("%d %s" % (n, s) for s, n in sorted(counts.items()))))
    print("next: verify with the same arguments, then take \"schedule_imports_enabled\" out of "
          "%s again." % os.path.join(args.state, "config.json"))
    return 0 if clean else 1


# ---------------------------------------------------------------- verify


def run_verify(args):
    files, ignored = read_source(args.source)
    daemon = Daemon(args.port, args.state)
    exported = daemon.identify()
    listed = daemon.get_json("/v1/orchestrator/schedules").get("schedules")
    if not isinstance(listed, list):
        raise Refused("The schedule list did not carry schedules.")
    invalid = {r.get("file"): r for r in listed if isinstance(r, dict) and r.get("state") == "invalid"}
    valid = {r.get("id"): r for r in listed if isinstance(r, dict) and r.get("id")}

    print("verify  %s against 127.0.0.1:%d (%s)" % (args.source, args.port, args.state))
    if ignored:
        print("ignored %d entr%s not named *.json: %s"
              % (len(ignored), "y" if len(ignored) == 1 else "ies", ", ".join(ignored)))
    width = max(len(name) for name, _, _ in files)
    print("  %-*s  %-9s  %-9s  %s" % (width, "file", "bytes", "parsed", "next_fire"))
    same = {"bytes": 0, "parsed": 0, "next_fire": 0}
    notes = []
    for name, source_raw, text in files:
        sid = name[:-len(".json")]
        # 1. Bytes.
        stored = exported.get(name)
        if stored is None:
            bytes_state = "MISSING"
            notes.append((name, "bytes: the daemon holds no file by this name"))
        elif sha(stored.encode("utf-8")) == sha(source_raw):
            bytes_state = "SAME"
        else:
            bytes_state = "DIFFERENT"
            notes.append((name, "bytes: source sha256 %s, stored %s"
                          % (sha(source_raw)[:12], sha(stored.encode("utf-8"))[:12])))
        # 2. What the daemon's parser read, against the source.
        source = source_object(text)
        detail = None
        if name in invalid:
            parsed_state = "INVALID"
            notes.append((name, "parsed: the daemon lists it as invalid: %s" % invalid[name].get("error")))
        elif sid in valid:
            t0 = time.time()
            status, raw = daemon.call("GET", "/v1/orchestrator/schedules/" + sid)
            t1 = time.time()
            if status != 200:
                raise Refused("GET the schedule %s was refused: %s" % (sid, refusal(status, raw)))
            detail = decode(raw, "the schedule %s" % sid).get("schedule") or {}
            if source is None:
                parsed_state = "DIFFERENT"
                notes.append((name, "parsed: the source is not a JSON object, but the daemon read a schedule"))
            else:
                fields = differing_fields(source, detail)
                parsed_state = "DIFFERENT" if fields else "SAME"
                if fields:
                    notes.append((name, "parsed: differs in " + ", ".join(fields)))
        else:
            parsed_state = "MISSING"
            notes.append((name, "parsed: the daemon lists no schedule by this id"))
        # 3. The next firing, computed here from the source's own `when`.
        when = parse_when(source.get("when")) if source is not None else None
        if detail is None:
            fire_state, fire_text = "-", ""
        elif when is None:
            fire_state, fire_text = "UNREADABLE", ""
            notes.append((name, "next_fire: the source `when` could not be read here"))
        else:
            have = detail.get("next_fire")
            # The daemon answered somewhere between t0 and t1; a firing that
            # falls between the two is still the same answer.
            wants = {next_fire(when, t0), next_fire(when, t1)}
            fire_state = "SAME" if have in wants else "DIFFERENT"
            fire_text = when_text(have)
            if fire_state == "DIFFERENT":
                notes.append((name, "next_fire: daemon %s, computed here %s"
                              % (when_text(have), " or ".join(when_text(w) for w in sorted(wants, key=str)))))
        for key, value in (("bytes", bytes_state), ("parsed", parsed_state), ("next_fire", fire_state)):
            same[key] += value == "SAME"
        print("  %-*s  %-9s  %-9s  %-9s %s" % (width, name, bytes_state, parsed_state, fire_state, fire_text))
    for name, note in notes:
        print("  %-*s  %s" % (width, name, note))
    extra = sorted(set(exported) - {name for name, _, _ in files})
    if extra:
        print("the daemon also holds %d file%s not in the source (not compared): %s"
              % (len(extra), "" if len(extra) == 1 else "s", ", ".join(extra)))
    n = len(files)
    print("%d file%s: bytes %d/%d SAME, parsed %d/%d SAME, next_fire %d/%d SAME"
          % (n, "" if n == 1 else "s", same["bytes"], n, same["parsed"], n, same["next_fire"], n))
    return 0 if all(v == n for v in same.values()) else 1


def port_number(text):
    try:
        port = int(text, 10)
    except ValueError:
        raise argparse.ArgumentTypeError("not a port: %r" % text)
    if not 0 < port < 65536:
        raise argparse.ArgumentTypeError("not a port: %r" % text)
    return port


def main(argv):
    parser = argparse.ArgumentParser(
        prog="migrate-schedules.py",
        description="Import the Swift app's schedule files into the new daemon, and verify them.")
    sub = parser.add_subparsers(dest="command", metavar="{import,verify}")
    sub.required = True
    for name, text in (("import", "send every *.json in the copy to the daemon"),
                       ("verify", "compare the copy with what the daemon holds, three ways; changes nothing")):
        p = sub.add_parser(name, help=text, description=text)
        p.add_argument("source", help="a copy of ~/.config/clawdline/schedules (never that directory itself)")
        p.add_argument("port", type=port_number, help="the new daemon's port")
        p.add_argument("state", help="the CLAWDLINE_NEXT_DIR that daemon was started with")
    args = parser.parse_args(argv)
    try:
        return run_import(args) if args.command == "import" else run_verify(args)
    except Refused as e:
        print("migrate-schedules: %s" % e, file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
