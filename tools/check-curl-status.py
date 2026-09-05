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

**Two scopes, one rule, and the line between them is who reads the result.**

*Shell that this repository executes*: tracked `*.sh` files and tracked files with a shell
shebang.

*Instructions this repository hands to an agent, which the agent then runs*: the string literals
of `Sources/**/*.swift` — every child briefing is written there — and `Resources/skill-guides/*.md`,
which ships inside the app bundle to everybody who installs Clawdline. Nobody reads a briefing but
the assistant it was written for, and it types what it is shown, so a `curl` there is a call and
not an illustration.

*Not scanned*: `docs/`, whose seventy-three `curl` lines are terminal transcripts written for a
person looking at the reply. `--fail` there would suppress the error body, which on an API
reference is the half being explained. `docs/curl-status.md` carries that reasoning.

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


def tracked_files():
    """Every tracked name under ROOT. Both scans start here, so both refuse the same silence."""
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
    return names


def shell_files():
    names = tracked_files()
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


def analyse(text, exempt_lines=None, require_arguments=False,
            unchecked_message=UNCHECKED_MESSAGE):
    """Every curl call in one piece of shell, with its verdict.

    Returns tuples of (start_line, end_line, verdict, detail, rendered, message). `verdict` is
    one of ok / exempt / unchecked / bad-exemption; `message` is None where there is no finding.

    `exempt_lines` is where the exemption markers are read from, which is the original file when
    what was analysed is a projection of it — a marker lives in a `//` comment or an HTML comment,
    neither of which survives being projected into shell. `require_arguments` drops a `curl` with
    nothing after it: in a shell script that is a call that would run, and in prose it is the name
    of a program.
    """
    out = []
    lines = exempt_lines if exempt_lines is not None else text.splitlines()
    for cmd in split_commands(text):
        words = command_words(cmd)
        if not is_curl(words):
            continue
        if require_arguments and len(words) < 2:
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
            message = unchecked_message
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



# ---------------------------------------------------------------------------------------------
# The other audience: text this repository hands to an agent, which the agent then runs.
#
# `docs/curl-status.md` draws the line at **who reads the result**, and that line is not
# "documentation is exempt". `docs/api.md` is seventy-three terminal transcripts written for a
# person who is looking at the answer, and `--fail` there would hide the error body — the half
# being explained. A child briefing is the other side of the same line: nobody reads it but the
# agent, the agent runs the command, and curl's exit status is the only thing between a `403` and
# "I sent it". Measured against this machine's own broker on 2026-09-05: a progress note posted
# with a wrong secret answers `403 forbidden` and `curl -s` exits **0**.
#
# Two sources carry that text and both of them ship:
#
#   * `Sources/**/*.swift` string literals — the app writes these into every child's terminal.
#   * `Resources/skill-guides/*.md` — copied into the app bundle, read by an assistant that then
#     types the commands. Every install of Clawdline gets this file, not only this Mac.
#
# **Doing and mentioning have to be told apart here too**, and the answer is the same as it was in
# shell: structure, not vocabulary. A command an agent runs is written as code — a fenced block
# tagged as a shell, or an inline code span. Prose that merely names the program is prose, and
# `curl` with nothing after it is a program's name rather than a command. So each file is projected
# into the shell it actually contains, blank everywhere else, and the same tokenizer reads it. The
# projection keeps every newline, so a finding's line number is the line in the file a person opens.

INSTRUCTION_SOURCES = (
    ("Resources/skill-guides/", ".md",
     "ships inside the app bundle; its reader is an assistant that will type these commands"),
    ("Sources/", ".swift",
     "string literals the app writes into an agent's terminal — a briefing is an instruction"),
)

# Same audience, outside the paths this change claimed, and named here rather than left to be
# discovered: `AGENTS.md` and `skills/clawdline/SKILL*.md` each carry an unchecked call. Whoever
# brings them into INSTRUCTION_SOURCES fixes those calls in the same commit.
#
# The list is checked rather than recited. A deferral nobody re-reads becomes folklore, so this one
# is refused the day it stops being true in either direction: a file here that has been brought
# into the scan, or one that is no longer in the tree, is a finding that says take it out. Same
# shape as a stale `# known-blind:` marker in `tools/check-guards-go-red.sh`, and for the same
# reason. Only against this checkout: a fixture root has its own tree and owes nothing to this
# repository's bookkeeping.
INSTRUCTION_NOT_YET = ("AGENTS.md", "skills/clawdline/SKILL.md", "skills/clawdline/SKILL.zh-TW.md")


def deferred_findings(scanned_names):
    """Findings about the deferral list itself, empty unless it has gone stale."""
    if os.environ.get("CLAWDLINE_CURL_SCAN_ROOT"):
        return []
    tracked = set(tracked_files())
    out = []
    for name in INSTRUCTION_NOT_YET:
        if name in scanned_names:
            out.append((name, 1, "is in INSTRUCTION_NOT_YET and is being scanned anyway. It was "
                                 "brought in; take it out of the list, in this commit."))
        elif name not in tracked:
            out.append((name, 1, "is in INSTRUCTION_NOT_YET and is not in this checkout any more. "
                                 "A deferral pointing at nothing reads as coverage and is none — "
                                 "take it out."))
    return out

# A fence tagged as a shell is a command. `json`, `text` and an untagged fence are what this
# repository shows answers in, and `console` would be a transcript — a person reading a reply.
SHELL_FENCE_LANGUAGES = {"bash", "sh", "shell", "zsh"}

FENCE_RE = re.compile(r"^\s*(`{3,}|~{3,})\s*([A-Za-z0-9_+.-]*)\s*$")

INSTRUCTION_MESSAGE = (
    "teaches a command that reads curl's exit status, which is 0 for 401, 404 and 409 alike — "
    "the agent that runs it will report a refusal as something it sent. Write "
    "--fail-with-body (the typed error body still prints and the command exits non-zero), or "
    "take the code out with -w '%{http_code}' and compare it, and say in the surrounding text "
    "what to do when it fails. If this example is deliberately unchecked, say so in place: # "
    + EXEMPT_MARKER + " <reason>")


def blanked(text):
    """`text` with every character replaced by a space, newlines kept.

    Every projection writes into one of these, which is what makes a reported line number the
    line number in the file rather than an offset into an extract nobody can open.
    """
    return "".join("\n" if ch == "\n" else " " for ch in text)


# An interpolation is a value, not punctuation. `\(task.id)` becomes a task id in the terminal, so
# the projection puts a same-length run of `x` where it stood: keeping the parentheses would end the
# command at the `)` — the tokenizer treats one as a command boundary — and the flags written after
# the URL would be invisible. That is a false *finding* rather than a false pass, which is the safe
# direction and still the wrong answer.
INTERPOLATION_FILLER = "x"


def copy_literal_char(text, out, i):
    """Copy one character of a string literal into the projection; return the next index."""
    n = len(text)
    if text[i] == "\\" and i + 1 < n and text[i + 1] == "(":
        depth = 0
        j = i + 1
        while j < n:
            if text[j] == "(":
                depth += 1
            elif text[j] == ")":
                depth -= 1
                if depth == 0:
                    j += 1
                    break
            elif text[j] == "\n":
                break
            j += 1
        for k in range(i, min(j, n)):
            if text[k] != "\n":
                out[k] = INTERPOLATION_FILLER
        return j
    if text[i] == "\\" and i + 1 < n and text[i + 1] in '\\"':
        # Swift's escape for a backslash is two characters and the projection has to stay the same
        # length, so the first becomes a space and the second stays: a briefing line ending in `\\`
        # ends in a real backslash by the time an agent reads it, and is a line continuation.
        out[i + 1] = text[i + 1]
        return i + 2
    out[i] = text[i]
    return i + 1


def swift_literal_projection(text):
    r"""The string literals of a Swift file, in place; comments and code blanked.

    Dropping comments drops most of what mentioning looks like in Swift: all six `curl` lines in
    `Sources/Orchestrator.swift` are `///`, and none of them is a command. What is kept is what
    the app *emits*, so the escapes come out on the way: `\\` at the end of a briefing line is a
    single backslash by the time an agent reads it, and therefore a shell line continuation —
    which is how a four-line `curl` in a briefing is read as one command instead of four. The
    two characters are replaced by a space and the character, so the projection stays the same
    length as the file and the line numbers stay true. `\(` is left alone: it is an
    interpolation, and leaving the backslash on it keeps the interpolated URL a single word.
    """
    out = list(blanked(text))
    i = 0
    n = len(text)
    while i < n:
        c = text[i]
        if c == "/" and i + 1 < n and text[i + 1] == "/":
            j = text.find("\n", i)
            i = n if j == -1 else j
            continue
        if c == "/" and i + 1 < n and text[i + 1] == "*":
            depth = 1
            i += 2
            while i < n and depth:
                if text[i:i + 2] == "/*":
                    depth += 1
                    i += 2
                    continue
                if text[i:i + 2] == "*/":
                    depth -= 1
                    i += 2
                    continue
                i += 1
            continue
        if c == "#" and i + 1 < n:
            # A raw string. Its delimiter carries the same number of `#`, and nothing inside it is
            # an escape — including a `"""` in the middle of one, which is the shape that walked
            # this parser out of step and hid every command after it in the file.
            j = i
            while j < n and text[j] == "#":
                j += 1
            hashes = j - i
            if j < n and text[j] == '"':
                triple = text[j:j + 3] == '"""'
                closing = ('"""' if triple else '"') + "#" * hashes
                body = j + (3 if triple else 1)
                k = text.find(closing, body)
                for m in range(body, n if k == -1 else k):
                    if text[m] != "\n":
                        out[m] = text[m]
                i = n if k == -1 else k + len(closing)
                continue
            i = j
            continue
        if text[i:i + 3] == '"""':
            i += 3
            while i < n and text[i:i + 3] != '"""':
                i = copy_literal_char(text, out, i)
            i += 3
            continue
        if c == '"':
            i += 1
            while i < n and text[i] != '"' and text[i] != "\n":
                i = copy_literal_char(text, out, i)
            i += 1
            continue
        i += 1
    return "".join(out)


def keep_code_spans(line, span_open):
    """One prose line with everything outside its inline code spans blanked.

    `span_open` carries the length of an unclosed backtick run in from the line before, because a
    code span may cross a line — the completion announce in `OrchestratorChildBrief.swift` is
    three lines inside one pair of backticks, and reading only single lines would miss it.

    Every backtick becomes a `;`, which is what keeps two spans on one line two commands rather
    than one. Blanking them instead joined the two mentions in "plain `curl` exits 0 whatever the
    server says: a `401` and a `200` are the same exit status" into the command `curl 401`, so the
    sentence explaining the defect was reported as an instance of it.
    """
    out = [" "] * len(line)
    i = 0
    n = len(line)
    while i < n:
        if line[i] == "`":
            j = i
            while j < n and line[j] == "`":
                out[j] = ";"
                j += 1
            run = j - i
            if span_open is None:
                span_open = run
            elif span_open == run:
                span_open = None
            i = j
            continue
        if span_open is not None:
            out[i] = line[i]
        i += 1
    return "".join(out), span_open


def markdown_command_projection(text):
    """The commands in a markdown document, in place; prose and answers blanked.

    Kept: fenced blocks tagged as a shell, and inline code spans. Blanked: prose, and fenced
    blocks in any other language — `json` and `text` are how this repository shows what came
    back, and a reply is not a command. A transcript written with a `$ ` prompt is left alone
    too, by the tokenizer rather than by this: the command it sees begins with `$`, not `curl`.
    """
    out_lines = []
    fence = None
    span_open = None
    for raw in text.split("\n"):
        if fence is not None:
            stripped = raw.strip()
            if stripped and set(stripped) == {fence[0]} and len(stripped) >= fence[1]:
                fence = None
                out_lines.append(blanked(raw))
                continue
            out_lines.append(raw if fence[2] else blanked(raw))
            continue
        opener = FENCE_RE.match(raw)
        if opener:
            marker = opener.group(1)
            fence = (marker[0], len(marker), opener.group(2).lower() in SHELL_FENCE_LANGUAGES)
            span_open = None
            out_lines.append(blanked(raw))
            continue
        if not raw.strip():
            # A code span cannot cross a blank line, and letting one do so would let a stray
            # backtick swallow the rest of a document.
            span_open = None
            out_lines.append(blanked(raw))
            continue
        kept, span_open = keep_code_spans(raw, span_open)
        out_lines.append(kept)
    return "\n".join(out_lines)


def instruction_files():
    """Tracked files whose reader is an agent that runs what it reads."""
    names = tracked_files()
    out = []
    for name in names:
        for prefix, suffix, why in INSTRUCTION_SOURCES:
            if name.startswith(prefix) and name.endswith(suffix):
                path = ROOT / name
                if path.is_file():
                    out.append((name, path, suffix, why))
                break
    return sorted(out)


def project(text, suffix):
    if suffix == ".swift":
        text = swift_literal_projection(text)
    return markdown_command_projection(text)


def scan_instructions():
    """Findings, listing, and the number of instruction files seen.

    The count comes back so that "nothing to look at" can be refused rather than reported as a
    pass: an extractor that quietly stops matching is exactly the shape of guard this whole
    family of checks exists because of.
    """
    findings = []
    listed = []
    sources = instruction_files()
    for name, path, suffix, _why in sources:
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        if "curl" not in text:
            continue
        projected = project(text, suffix)
        if "curl" not in projected:
            continue
        rows = analyse(projected, exempt_lines=text.splitlines(),
                       require_arguments=True, unchecked_message=INSTRUCTION_MESSAGE)
        for start, end, verdict, detail, rendered, message in rows:
            if message:
                findings.append((name, start, message))
            listed.append((name, start, end, verdict, detail, rendered))
    return findings, listed, len(sources)

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


# And the same enumeration for the text this repository hands to agents. The negatives here are
# the ones the first domain never had to answer: a reply block, a prompt-prefixed transcript, and
# the word `curl` inside a sentence — all three sit in these very files, next to real commands.
INSTRUCTION_SELF_TEST = [
    # --- markdown an assistant reads and types ------------------------------------------------
    (".md", "```bash\ncurl -s http://x\n```", ["unchecked"],
     "the shape every guide call is written in"),
    (".md", "```bash\ncurl --fail-with-body -sS http://x\n```", ["ok"],
     "the flag that keeps the typed error body and still fails"),
    (".md", "```bash\ncurl -sS -o /dev/null -w '%{http_code}' http://x\n```", ["ok"],
     "or the code taken out to be compared"),
    (".md", "   ```bash\n   curl -s http://x\n   ```", ["unchecked"],
     "a fence indented inside a numbered list is still a fence"),
    (".md", "```json\n{\"cmd\":\"curl -s http://x\"}\n```", [],
     "a reply block is what came back, not a command"),
    (".md", "```\ncurl -s http://x\n```", [],
     "an untagged fence is how this repository shows output"),
    (".md", "- `curl` cannot connect -> the server is not running.", [],
     "the program's name in a sentence"),
    (".md", "a `curl` to 127.0.0.1 exits 7 after 0 ms, and DNS is off too", [],
     "the same, with the prose either side of it"),
    (".md", "plain `curl` exits 0 whatever the server says: a `401` and a `200` are the same", [],
     "two mentions on one line are two spans; blanking the backticks made them one command"),
    (".md", "curl -s http://x is what the old build did", [],
     "prose is prose even when it parses as a command"),
    (".md", "**Check quota first.** `curl -s http://127.0.0.1:7717/v1/health`", ["unchecked"],
     "an inline span with arguments is a command an agent will type"),
    (".md", "run `curl --fail-with-body -sS http://x` before dispatching", ["ok"],
     "and it is judged the same way"),
    (".md", "`curl -s -X POST http://x \\\n  -H 'Content-Type: application/json' \\\n  -d '{}'`",
     ["unchecked"], "a code span may cross lines — the completion announce is three of them"),
    (".md", "```bash\n$ curl -s http://x\n{\"ok\":true}\n```", [],
     "a `$ ` prompt is a transcript: the command begins with $, not with curl"),
    (".md", "<!-- curl-status-exempt: the point of this example is the error body itself -->\n"
            "```bash\ncurl -s http://x\n```", ["exempt"],
     "an exemption in an HTML comment above the block"),
    (".md", "<!-- curl-status-exempt: legacy -->\n```bash\ncurl -s http://x\n```",
     ["bad-exemption"], "a marker with nothing behind it is a silencer here too"),
    # --- Swift that writes a briefing into somebody's terminal --------------------------------
    (".swift", 'let brief = """\n```bash\ncurl -s http://x\n```\n"""', ["unchecked"],
     "a fenced command inside a multi-line string literal"),
    (".swift",
     'let brief = """\n  ```bash\n  curl -s http://x \\\\\n    --fail-with-body -o out\n  ```\n"""',
     ["ok"],
     "the escaped backslash is a continuation once the app has printed it: one command, not two"),
    (".swift",
     'let brief = """\n  ```bash\n  curl http://127.0.0.1:\\(port)/v1/x --fail-with-body -sS\n  ```\n"""',
     ["ok"],
     "an interpolation is a value: the flags written after the URL are still part of the command"),
    (".swift", 'let brief = """\n`curl` cannot connect -- some sandboxes have no loopback\n"""',
     [], "prose inside a literal is still prose"),
    (".swift", "// curl -s http://x is what this used to do", [],
     "a line comment is not something the app emits"),
    (".swift", "/// A leaf spends a turn on a curl to 127.0.0.1 that cannot connect", [],
     "and neither is a doc comment"),
    (".swift", '/* curl -s http://x */\nlet x = 1', [], "nor a block comment"),
    (".swift", 'let cmd = "curl -s http://x"', [],
     "a one-line literal carries no code block, so it is out of this domain and named in the docs"),
    (".swift",
     'let t = #"""\n"""\nx\n"""#\nlet brief = """\n  ```bash\n  curl -s http://x\n  ```\n  """',
     ["unchecked"],
     "a raw string holding a bare triple quote walked this parser out of step and hid the rest"),
]

def self_test():
    failures = 0
    for source, expected, why in SELF_TEST:
        got = [row[2] for row in analyse(source)]
        if got != expected:
            failures += 1
            sys.stderr.write("curl status guard self-test: %s\n  expected %s, got %s\n  %s\n"
                             % (why, expected, got, source.replace("\n", "\\n")))
    for suffix, source, expected, why in INSTRUCTION_SELF_TEST:
        rows = analyse(project(source, suffix), exempt_lines=source.splitlines(),
                       require_arguments=True, unchecked_message=INSTRUCTION_MESSAGE)
        got = [row[2] for row in rows]
        if got != expected:
            failures += 1
            sys.stderr.write("curl status guard self-test (%s instructions): %s\n"
                             "  expected %s, got %s\n  %s\n"
                             % (suffix, why, expected, got, source.replace("\n", "\\n")))
    total = len(SELF_TEST) + len(INSTRUCTION_SELF_TEST)
    if failures:
        sys.stderr.write("%d of %d self-test cases wrong — the scanner is what changed, not "
                         "the tree.\n" % (failures, total))
        return 1
    print("curl status guard self-test: %d shapes in shell and %d in the instructions this app "
          "hands to agents, calls and mentions told apart"
          % (len(SELF_TEST), len(INSTRUCTION_SELF_TEST)))
    return 0


def main():
    if "--self-test" in sys.argv[1:]:
        return self_test()

    findings, listed = scan()
    told, told_listed, told_sources = scan_instructions()

    if "--list" in sys.argv[1:]:
        for domain, rows in (("shell", listed), ("agent", told_listed)):
            for name, start, end, verdict, detail, rendered in rows:
                span = str(start) if start == end else "%d-%d" % (start, end)
                print("%-6s %-40s %-8s %-10s %s" % (domain, "%s:%s" % (name, span), verdict,
                                                    detail[:10], rendered[:110]))
        print("\n%d curl calls in tracked shell and %d in the instructions this app hands to "
              "agents (%d files); %d findings"
              % (len(listed), len(told_listed), told_sources, len(findings) + len(told)))
        return 0

    if not listed:
        # Same refusal as an empty file list: this repository has curl calls in shell, and a
        # scan that finds none has stopped working rather than come good.
        sys.stderr.write("curl_scan_found_nothing: no curl call in any tracked shell script "
                         "under %s — the scanner, not the tree, is what changed.\n" % ROOT)
        return 2

    stale = deferred_findings({name for name, _p, _s, _w in instruction_files()})
    if findings or told or stale:
        sys.stderr.write("curl status guard: a failed request must not read as a successful one.\n")
        for name, line, message in findings + told + stale:
            sys.stderr.write("  %s:%d: %s\n" % (name, line, message))
        sys.stderr.write("  %d finding(s). docs/curl-status.md has the rule, the exemption form "
                         "and the line between a command and a transcript.\n"
                         % (len(findings) + len(told) + len(stale)))
        return 1

    # The findings come first and these refusals after, because a structural complaint about the
    # scan is less use to a reader than the defect the scan found. Both refusals are the same
    # shape as the empty file list above: this repository *has* briefings with commands in them,
    # so a projection that produces none of them has stopped working rather than come good.
    if not told_sources:
        sys.stderr.write("curl_instruction_scan_no_sources: nothing tracked under %s matches the "
                         "instruction sources (%s) — the selector, not the tree.\n"
                         % (ROOT, ", ".join(prefix for prefix, _s, _w in INSTRUCTION_SOURCES)))
        return 2
    if not told_listed:
        sys.stderr.write("curl_instruction_scan_found_nothing: %d instruction file(s) under %s and "
                         "no command in any of them — the projection, not the tree.\n"
                         % (told_sources, ROOT))
        return 2

    print("curl status guard: %d curl calls in tracked shell and %d in the briefings and guides "
          "this app hands to agents, every one of them can tell a refusal from an answer"
          % (len(listed), len(told_listed)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
