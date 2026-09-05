#!/usr/bin/env python3
r"""Every `curl` this repository *runs* must be able to tell a failure from a success.

`curl` exits 0 for `409`, for `401`, for `503`. On 2026-09-05 that cost this machine two
separate incidents in one evening, in the same file: `build.sh` read a refused restart as an
accepted one, and — one layer down — read its own five-second client timeout as the server
saying no. Both times the caller was reading curl's exit status, and both times that status
was a constant.

So: a `curl` invocation is compliant when it does one of two things.

  1. Asks curl to fail on an HTTP error — `--fail`, `--fail-with-body`, or a short cluster
     carrying `-f` (`-fsSL`).
  2. Takes the status code out explicitly — `-w`/`--write-out` with `%{http_code}` (or
     `%{response_code}`) in the format — so the caller has something to compare.

**What this guard can and cannot decide.** It can see that the code was extracted. It cannot
see that anybody compared it: proving that needs dataflow this does not have, and a check that
claims more than it verified is the defect it is named after. `-w '%{http_code}'` therefore
buys a pass here and is still worth a reviewer's eye. What it does buy is real all the same —
the two incidents above were both "the status was never asked for", not "the status was asked
for and then ignored".

**Exemptions are written in place, next to the call, with the reason.** A `curl` that is meant
to ignore the answer is a real thing — a liveness probe wants to know whether anything is
listening, and a `500` means yes. Write

    # curl-status-exempt: <why this call does not care what the server answered>

on the call, or in the few lines above it. A list of exempt line numbers kept somewhere else
would go stale silently and would put the reason where the next reader is not looking.

**Detection is by tokenizing the shell, not by matching a pattern.** Every real call in this
repository is spread over three to six backslash-continued lines with quoted JSON in it, and a
regular expression over that either misses the flags or invents them. This walks the file with
the quoting rules — `'`, `"`, `\`, `#`, heredocs, `$( )` — splits it into commands, and asks
each command that *is* a `curl` what flags it was given. The negative examples that motivate
this are in `docs/curl-status.md`: `command -v curl`, `pgrep -x curl`, the word "curl" in
ninety-odd comments, a fake `curl` written into a test's PATH, and `curl` inside a heredoc that
is documentation rather than a command.

**Scope: shell that this repository executes.** Tracked `*.sh` files and tracked files with a
shell shebang. Documentation is deliberately not scanned; `docs/curl-status.md` says why, and
says what a documented example owes instead.

Usage:
    tools/check-curl-status.py            # check the tree, exit 1 on a finding
    tools/check-curl-status.py --list     # print every curl call found, with its verdict

Root override for the red proof: CLAWDLINE_CURL_SCAN_ROOT.
"""

import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(os.environ.get("CLAWDLINE_CURL_SCAN_ROOT",
                           Path(__file__).resolve().parent.parent)).resolve()

EXEMPT_MARKER = "curl-status-exempt:"
# How far above a call an exemption may sit. A multi-line call has its marker above the first
# line; anything further away than a short comment block is a list kept somewhere else wearing
# a comment's clothes.
EXEMPT_LOOKBACK = 6
# A marker with nothing after it is a silencer, not an exemption.
EXEMPT_MIN_REASON = 16

FAIL_LONG = {"--fail", "--fail-with-body", "--fail-early"}
WRITE_OUT_SHORT = "w"
STATUS_TOKENS = ("%{http_code}", "%{response_code}")

# curl short options that consume the next word (or the rest of their cluster). Anything not in
# here is a flag, which is what lets `-fsSL` be read as "carries -f" and `-o body` be read as
# "-o takes body" rather than as two flags. Wrong in either direction is visible: a value option
# missing from here turns its value into a bag of letters (and `-f` inside a URL would be a false
# pass), a flag wrongly listed here swallows the word after it (and a real `-f` would be missed).
SHORT_WITH_VALUE = set("AbCcDdEeFHKmoPQrTtUuwXxYyz")
LONG_WITH_VALUE = {
    "--output", "--write-out", "--header", "--data", "--data-raw", "--data-binary",
    "--data-urlencode", "--request", "--user", "--user-agent", "--referer", "--connect-timeout",
    "--max-time", "--retry", "--retry-delay", "--retry-max-time", "--url", "--form",
    "--form-string", "--cookie", "--cookie-jar", "--dump-header", "--proxy", "--interface",
    "--cert", "--key", "--cacert", "--capath", "--resolve", "--range", "--upload-file",
    "--limit-rate", "--max-filesize", "--oauth2-bearer", "--unix-socket", "--expect100-timeout",
    "--happy-eyeballs-timeout-ms", "--output-dir", "--proto", "--tlsv1", "--ciphers",
}

# Words that may stand in front of a command without being the command.
COMMAND_PREFIXES = {"env", "command", "sudo", "time", "exec", "nohup", "builtin", "then", "do",
                    "else", "elif", "if", "while", "until", "!", "{", "}"}


class Word:
    """One shell word, with the two facts about it the caller needs."""

    __slots__ = ("text", "quoted", "line")

    def __init__(self, text, quoted, line):
        self.text = text
        self.quoted = quoted          # any part of it came out of quotes
        self.line = line              # 1-based line the word started on

    def __repr__(self):
        return "Word(%r, quoted=%s, line=%d)" % (self.text, self.quoted, self.line)


class Command:
    __slots__ = ("words", "start_line", "end_line")

    def __init__(self, words):
        self.words = words
        self.start_line = words[0].line
        self.end_line = words[-1].line


def split_commands(text):
    """Walk shell source and return the commands in it.

    Not a shell. It knows only what this guard needs: where a word ends, where a command ends,
    and which characters are quoted. Everything it does not understand it treats as a command
    boundary, which errs towards splitting a call in two — a truncated `curl` command loses
    flags and is reported, so the failure mode is a finding a person reads, never a silent pass.
    """
    commands = []
    word_chars = []
    word_quoted = False
    word_line = 1
    current = []
    line = 1
    i = 0
    n = len(text)
    pending_heredocs = []

    def end_word():
        nonlocal word_chars, word_quoted, word_line
        if word_chars:
            current.append(Word("".join(word_chars), word_quoted, word_line))
        word_chars = []
        word_quoted = False

    def end_command():
        nonlocal current
        end_word()
        if current:
            commands.append(Command(current))
        current = []

    while i < n:
        c = text[i]

        if c == "\n":
            line += 1
            end_command()
            i += 1
            # A heredoc body is data, not shell. Skip to its terminator.
            while pending_heredocs:
                delim, strip = pending_heredocs.pop(0)
                while i < n:
                    nl = text.find("\n", i)
                    if nl == -1:
                        nl = n
                    raw = text[i:nl]
                    candidate = raw.strip() if strip else raw
                    i = nl + 1
                    line += 1
                    if candidate == delim:
                        break
                    if i >= n:
                        break
            continue

        if c == "\\":
            if i + 1 < n and text[i + 1] == "\n":
                # Line continuation: the command carries on, the line number does not.
                line += 1
                i += 2
                continue
            if i + 1 < n:
                word_chars.append(text[i + 1])
                if not word_chars[:-1]:
                    word_line = line
                i += 2
                continue
            i += 1
            continue

        if c == "'":
            if not word_chars:
                word_line = line
            word_quoted = True
            j = text.find("'", i + 1)
            if j == -1:
                j = n
            body = text[i + 1:j]
            line += body.count("\n")
            word_chars.append(body)
            i = j + 1
            continue

        if c == '"':
            if not word_chars:
                word_line = line
            word_quoted = True
            i += 1
            while i < n:
                d = text[i]
                if d == "\\" and i + 1 < n:
                    if text[i + 1] == "\n":
                        line += 1
                    else:
                        word_chars.append(text[i + 1])
                    i += 2
                    continue
                if d == '"':
                    i += 1
                    break
                if d == "\n":
                    line += 1
                word_chars.append(d)
                i += 1
            continue

        if c == "#" and not word_chars:
            j = text.find("\n", i)
            i = j if j != -1 else n
            continue

        if c in " \t":
            end_word()
            i += 1
            continue

        if c in ";&|()":
            end_command()
            i += 1
            continue

        if c == "`":
            end_command()
            i += 1
            continue

        if c == "$" and i + 1 < n and text[i + 1] == "(":
            # `$(` opens a command of its own; `$((` is arithmetic and has no command in it.
            if i + 2 < n and text[i + 2] == "(":
                depth = 0
                while i < n:
                    if text[i] == "(":
                        depth += 1
                    elif text[i] == ")":
                        depth -= 1
                        if depth == 0:
                            i += 1
                            break
                    elif text[i] == "\n":
                        line += 1
                    i += 1
                continue
            end_command()
            i += 2
            continue

        if c == "<" and text[i:i + 2] == "<<" and text[i:i + 3] != "<<<":
            j = i + 2
            strip = False
            if j < n and text[j] == "-":
                strip = True
                j += 1
            while j < n and text[j] in " \t":
                j += 1
            quote = ""
            if j < n and text[j] in "'\"":
                quote = text[j]
                j += 1
            k = j
            while k < n and (text[k].isalnum() or text[k] in "_-." or (quote and text[k] != quote)):
                k += 1
            delim = text[j:k]
            if quote and k < n and text[k] == quote:
                k += 1
            if delim:
                pending_heredocs.append((delim, strip))
            i = k
            continue

        if not word_chars:
            word_line = line
        word_chars.append(c)
        i += 1

    end_command()
    return commands


def command_words(cmd):
    """The words of a command with assignments and harmless prefixes stripped off the front."""
    words = list(cmd.words)
    while words:
        first = words[0]
        # The whole word goes, not just the `NAME=` prefix. `URL=http://x curl -s "$URL"` then
        # starts at `curl`, and `X="curl -s url"` — one quoted word — is left as a word that is
        # not the command `curl`, which is the point of dropping it whole.
        if re.match(r"^[A-Za-z_][A-Za-z_0-9]*=", first.text):
            words.pop(0)
            continue
        if first.text in COMMAND_PREFIXES:
            words.pop(0)
            continue
        break
    return words


def is_curl(words):
    if not words:
        return False
    name = words[0].text
    return name == "curl" or name.endswith("/curl")


def classify(words):
    """Return (has_fail, has_status_extraction) for one curl command's words."""
    has_fail = False
    has_status = False
    i = 1
    while i < len(words):
        w = words[i]
        text = w.text
        if w.quoted or not text.startswith("-") or text == "-":
            i += 1
            continue

        if text.startswith("--"):
            name, _, inline = text.partition("=")
            if name in FAIL_LONG:
                has_fail = True
            if name == "--write-out":
                value = inline if inline else (words[i + 1].text if i + 1 < len(words) else "")
                if any(tok in value for tok in STATUS_TOKENS):
                    has_status = True
                if not inline:
                    i += 1
            elif name in LONG_WITH_VALUE and not inline:
                i += 1
            i += 1
            continue

        # A short cluster: read it the way getopt does, so `-sw '%{http_code}'` and `-fsSL` are
        # both understood and neither is read as a bag of unrelated letters.
        cluster = text[1:]
        j = 0
        while j < len(cluster):
            ch = cluster[j]
            if ch == "f":
                has_fail = True
            if ch in SHORT_WITH_VALUE:
                rest = cluster[j + 1:]
                if rest:
                    value = rest
                else:
                    value = words[i + 1].text if i + 1 < len(words) else ""
                    i += 1
                if ch == WRITE_OUT_SHORT and any(tok in value for tok in STATUS_TOKENS):
                    has_status = True
                break
            j += 1
        i += 1
    return has_fail, has_status


def exemption_for(lines, cmd):
    """The in-place exemption covering this call, or None."""
    lo = max(0, cmd.start_line - 1 - EXEMPT_LOOKBACK)
    hi = min(len(lines), cmd.end_line)
    for idx in range(lo, hi):
        line = lines[idx]
        pos = line.find(EXEMPT_MARKER)
        if pos == -1:
            continue
        reason = line[pos + len(EXEMPT_MARKER):].strip().rstrip("\"'")
        return idx + 1, reason
    return None


def shell_files():
    listing = subprocess.run(["git", "-C", str(ROOT), "ls-files", "-z"],
                             capture_output=True, text=True)
    if listing.returncode != 0:
        sys.stderr.write("curl_scan_no_git: could not list tracked files under %s\n" % ROOT)
        sys.exit(2)
    names = [n for n in listing.stdout.split("\0") if n]
    if not names:
        # An empty scan is the shape every one of these guards has to refuse: it passes, and it
        # passes because it looked at nothing.
        sys.stderr.write("curl_scan_empty: git listed no tracked files under %s\n" % ROOT)
        sys.exit(2)
    out = []
    for name in names:
        path = ROOT / name
        if not path.is_file():
            continue
        if name.endswith(".sh"):
            out.append((name, path))
            continue
        try:
            with open(path, "rb") as fh:
                head = fh.readline(200)
        except OSError:
            continue
        if head.startswith(b"#!") and re.search(rb"\b(bash|sh|zsh|dash)\b", head):
            out.append((name, path))
    return sorted(out)


UNCHECKED_MESSAGE = (
    "reads curl's exit status, which is 0 for 409, 401 and 503 alike. "
    "Add --fail (or --fail-with-body), or take the code out with "
    "-w '%{http_code}' and compare it. If this call is meant not to "
    "care, say so in place: # " + EXEMPT_MARKER + " <reason>")


def analyse(text):
    """Every curl call in one piece of shell, with its verdict.

    Returns tuples of (start_line, end_line, verdict, detail, rendered, message). `verdict` is
    one of ok / exempt / unchecked / bad-exemption; `message` is None where there is no finding.
    """
    out = []
    lines = text.splitlines()
    for cmd in split_commands(text):
        words = command_words(cmd)
        if not is_curl(words):
            continue
        has_fail, has_status = classify(words)
        exemption = exemption_for(lines, cmd)
        message = None
        if has_fail:
            verdict, detail = "ok", "--fail"
        elif has_status:
            verdict, detail = "ok", "-w %{http_code}"
        elif exemption and len(exemption[1]) >= EXEMPT_MIN_REASON:
            verdict, detail = "exempt", exemption[1]
        elif exemption:
            verdict, detail = "bad-exemption", exemption[1]
            message = ("the exemption at line %d gives no reason. Write what this call does with "
                       "a refusal, in at least %d characters."
                       % (exemption[0], EXEMPT_MIN_REASON))
        else:
            verdict, detail = "unchecked", ""
            message = UNCHECKED_MESSAGE
        out.append((cmd.start_line, cmd.end_line, verdict, detail,
                    " ".join(w.text for w in words), message))
    return out


def scan():
    findings = []
    listed = []
    for name, path in shell_files():
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        if "curl" not in text:
            continue
        for start, end, verdict, detail, rendered, message in analyse(text):
            if message:
                findings.append((name, start, message))
            listed.append((name, start, end, verdict, detail, rendered))
    return findings, listed


# The enumeration this guard was asked for, kept as a test rather than as a paragraph: every
# shape of `curl` that is actually written in this repository, and — the half that a regular
# expression gets wrong — every shape that merely contains the word. A pattern cannot tell doing
# from mentioning, so each negative below is a line that would match `grep curl` and must not be
# read as a call.
SELF_TEST = [
    # --- calls, and what each one is owed -------------------------------------------------
    ("curl -s http://x", ["unchecked"], "the bare form both incidents were written in"),
    ("curl -fsSL -o out http://x", ["ok"], "-f inside a short cluster"),
    ("curl --fail-with-body http://x", ["ok"], "the long form that keeps the body"),
    ("S=$(curl -sS -o /dev/null -w '%{http_code}' http://x)", ["ok"], "code taken out explicitly"),
    ("curl --write-out '%{http_code}' -o /dev/null http://x", ["ok"], "the long spelling"),
    ("curl -sw '%{http_code}' -o /dev/null http://x", ["ok"], "-w as the tail of a cluster"),
    ("curl -s -o f -w '%{response_code}' http://x", ["ok"], "the other name for the same code"),
    ("STATUS=$(curl -sS \\\n  --max-time 2 \\\n  -o \"$B\" -w '%{http_code}' \\\n  http://x)",
     ["ok"], "five lines of backslash continuation — the shape every real call here has"),
    ("if ! curl -f http://x; then\n  echo no\nfi", ["ok"], "a call used as a condition"),
    ("curl -w '%{time_total}' http://x", ["unchecked"], "-w without the status in it"),
    ("curl -o out -H \"X-Try: -f\" http://x", ["unchecked"],
     "a -f inside a quoted header value is not a flag"),
    ("curl -d '{\"mode\":\"-f\"}' -X POST http://x", ["unchecked"],
     "and neither is one inside a quoted JSON body"),
    ("curl -F file=@x http://x", ["unchecked"], "-F is not -f"),
    ("curl -s http://x | jq .", ["unchecked"], "a pipeline does not launder the exit status"),
    ("curl -s http://a && curl -f http://b", ["unchecked", "ok"],
     "two calls in one AND-list are two subjects"),
    ("# curl-status-exempt: only asking whether anything is listening at all\ncurl -s http://x",
     ["exempt"], "an exemption above the call"),
    ("curl -s http://x  # curl-status-exempt: nothing here reads the answer, by design",
     ["exempt"], "an exemption on the call"),
    ("# curl-status-exempt: legacy\ncurl -s http://x", ["bad-exemption"],
     "a marker with no reason behind it is a silencer"),
    # --- not calls ------------------------------------------------------------------------
    ("command -v curl >/dev/null 2>&1", [], "asking whether curl exists"),
    ("if command -v curl >/dev/null; then echo yes; fi", [], "the same, as a condition"),
    ("pgrep -x curl", [], "the word as an argument"),
    ("# curl -s http://x is what this used to do", [], "the word in a comment"),
    ("echo \"run curl -s http://x yourself\"", [], "the word inside a quoted string"),
    ("CMD=\"curl -s http://x\"", [], "the word inside an assignment's value"),
    ("grep -n 'curl' build.sh", [], "the word as a pattern"),
    ("cat <<'EOF'\ncurl -s http://x\nEOF", [], "the word inside a quoted heredoc"),
    ("cat <<EOF\ncurl -s http://x\nEOF", [], "and inside an expanding one"),
    ("cat <<-EOF\n\tcurl -s http://x\n\tEOF", [], "and inside an indented one"),
    ("echo 'curl' > /dev/null\ncurl -f http://x", ["ok"],
     "a mention on one line does not hide the call on the next"),
]


def self_test():
    failures = 0
    for source, expected, why in SELF_TEST:
        got = [row[2] for row in analyse(source)]
        if got != expected:
            failures += 1
            sys.stderr.write("curl status guard self-test: %s\n  expected %s, got %s\n  %s\n"
                             % (why, expected, got, source.replace("\n", "\\n")))
    if failures:
        sys.stderr.write("%d of %d self-test cases wrong — the scanner is what changed, not "
                         "the tree.\n" % (failures, len(SELF_TEST)))
        return 1
    print("curl status guard self-test: %d shapes, calls and mentions told apart"
          % len(SELF_TEST))
    return 0


def main():
    if "--self-test" in sys.argv[1:]:
        return self_test()

    findings, listed = scan()

    if "--list" in sys.argv[1:]:
        for name, start, end, verdict, detail, rendered in listed:
            span = str(start) if start == end else "%d-%d" % (start, end)
            print("%-40s %-8s %-10s %s" % ("%s:%s" % (name, span), verdict,
                                           detail[:10], rendered[:120]))
        print("\n%d curl calls in tracked shell; %d findings" % (len(listed), len(findings)))
        return 0

    if not listed:
        # Same refusal as an empty file list: this repository has curl calls in shell, and a
        # scan that finds none has stopped working rather than come good.
        sys.stderr.write("curl_scan_found_nothing: no curl call in any tracked shell script "
                         "under %s — the scanner, not the tree, is what changed.\n" % ROOT)
        return 2

    if findings:
        sys.stderr.write("curl status guard: a failed request must not read as a successful one.\n")
        for name, line, message in findings:
            sys.stderr.write("  %s:%d: %s\n" % (name, line, message))
        sys.stderr.write("  %d call(s). docs/curl-status.md has the rule and the exemption form.\n"
                         % len(findings))
        return 1

    print("curl status guard: %d curl calls in tracked shell, every one of them can tell a "
          "refusal from an answer" % len(listed))
    return 0


if __name__ == "__main__":
    sys.exit(main())
