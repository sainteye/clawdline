#!/bin/sh
# Clawdline's bridge out of Claude Code.
#
# Installed by the app into ~/.config/clawdline/hook.sh and wired up in ~/.claude/settings.json.
# Safe to delete by hand: Clawdline goes back to reading the screen, which is what it does when
# this is not here at all.
#
# One job — leave a note saying what this session just did, in a file named after the terminal it
# is running on. The app is watching that directory, so the note lands in milliseconds instead of
# at the next poll. Nothing queues: a note is overwritten by the next one, so an app that was not
# running missed nothing it could still act on.
#
# Two rules, both about never making Claude Code worse than it was without this:
#
#   - **Nothing on stdout, ever.** A hook's stdout is read back as instructions, so a stray
#     `echo` here is a sentence typed into somebody's session.
#   - **Always exit 0.** A non-zero exit from a hook is a decision about the work in progress —
#     2 blocks it outright. Whatever goes wrong in here, it is not worth stopping somebody's
#     turn over.

event="$1"
[ -n "$event" ] || exit 0
kind="$2"

# Old settings written by an earlier Clawdline pass only the event. Keep those harmless until
# the next Install refreshes their command lines; an unfiltered Notification is only a look.
if [ -z "$kind" ]; then
    case "$event" in
        SessionStart) kind="session_start" ;;
        UserPromptSubmit) kind="user_prompt_submit" ;;
        Stop) kind="stop" ;;
        Notification) kind="idle_prompt" ;;
        SessionEnd) kind="session_end" ;;
        *) exit 0 ;;
    esac
fi

# These strings go into JSON without escaping because they come from our own command line. Refuse
# anything else so invoking the script by hand cannot turn an argument into malformed JSON.
case "$event" in
    SessionStart|UserPromptSubmit|Stop|PreToolUse|PostToolUse|PermissionRequest|Notification|SessionEnd) ;;
    *) exit 0 ;;
esac
case "$kind" in
    session_start|user_prompt_submit|stop|ask_user_question|ask_user_question_done|permission_request|permission_prompt|idle_prompt|agent_needs_input|notification_seen|session_end) ;;
    *) exit 0 ;;
esac

dir="${CLAWDLINE_HOOK_DIR:-$HOME/.config/clawdline/hooks}"
# The app makes this directory. No directory means nobody is listening, and the cheapest
# possible answer to that is to stop here.
[ -d "$dir" ] || exit 0

# Drained even where it is barely used: the other end is writing into a pipe, and a reader that
# walks away leaves it holding a write that will not complete.
payload=$(cat)

# The session id, cut out of the payload without parsing it. That is safe for exactly this field
# and would not be for any other: a uuid has no character in it that JSON would have escaped, so
# what is between the quotes is what was meant. Everything else in there — a path, a prompt — can
# contain a quote or a backslash, and none of it is wanted here.
session=""
case "$payload" in
    *'"session_id":"'*)
        session=${payload#*'"session_id":"'}
        session=${session%%'"'*}
        ;;
esac
case "$session" in
    "" | *[!0-9a-fA-F-]*) session="" ;;
esac
[ "${#session}" -eq 36 ] || session=""

# Which terminal this session is on.
#
# Not asked of this process: Claude Code starts its hooks in a session of their own, with no
# controlling terminal at all — `ps` says `??` for every one of them. So walk up the parents
# until a process turns up that has one. That process is Claude Code itself, and the tty it is
# sitting on is the same string iTerm2 reports for its tab and tmux reports for its pane, which
# is the whole reason this is the key: it is the one name both ends already agree on.
#
# Remembered per session afterwards, because the answer is a fact about a process that has
# already started and cannot change while it runs.
tty=""
[ -n "$session" ] && [ -r "$dir/.tty-$session" ] && tty=$(cat "$dir/.tty-$session")
if [ -z "$tty" ]; then
    pid=$$
    hops=0
    while [ "$hops" -lt 12 ]; do
        set -- $(ps -o ppid=,tty= -p "$pid" 2>/dev/null)
        [ $# -ge 2 ] || break
        case "$2" in
            ttys*) tty="$2"; break ;;
        esac
        [ "$1" -gt 1 ] 2>/dev/null || break
        pid="$1"
        hops=$((hops + 1))
    done
    [ -n "$tty" ] && [ -n "$session" ] && printf '%s' "$tty" > "$dir/.tty-$session" 2>/dev/null
fi
# **A census, not a state.** `notification_seen` is registered without a matcher, so it receives
# every notification Claude Code raises and appends the type to a log instead of writing a note.
# It exists to answer one question that no filtered registration can: what, if anything, is
# raised at the moment a picker opens. `PreToolUse` filtered to the tool is measured not to fire,
# `PostToolUse` on the same tool fires on the answer, and the gate that decides whether a
# flush-left caret counts is meanwhile opened by permission traffic auto mode generates on its
# own. Until the real name is known, that gate is guesswork wearing a signal's clothes.
#
# It writes nothing a reading depends on, so it cannot make any state worse; the log is capped so
# it cannot grow without bound either.
if [ "$kind" = "notification_seen" ]; then
    seen=$(printf '%s' "$payload" \
        | /usr/bin/plutil -extract notification_type raw -o - - 2>/dev/null)
    [ -n "$seen" ] || seen=$(printf '%s' "$payload" \
        | /usr/bin/plutil -extract message raw -o - - 2>/dev/null | /usr/bin/head -c 60)
    [ -n "$seen" ] || seen="(no type field)"
    log="$dir/notification-census.log"
    printf '%s %s %s\n' "$(date +%H:%M:%S)" "${tty:-no-tty}" "$seen" >> "$log" 2>/dev/null
    lines=$(/usr/bin/wc -l < "$log" 2>/dev/null | /usr/bin/tr -d ' ')
    if [ "${lines:-0}" -gt 400 ] 2>/dev/null; then
        /usr/bin/tail -n 200 "$log" > "$log.trim" 2>/dev/null && /bin/mv "$log.trim" "$log"
    fi
    exit 0
fi

# No tty, no note. There is nothing to key it on, and a note nobody can match to a session on
# screen is worse than none: the app would show it as installed and working while telling you
# nothing. Screen reading covers this case, as it covers every case.
[ -n "$tty" ] || exit 0

# An idle notification must never erase an authoritative question that is still unanswered.
# PostToolUse replaces the AskUserQuestion note when the answer lands; until then the older note
# is the state, while idle_prompt is only a request to look at the same screen again.
if [ "$kind" = "idle_prompt" ] && [ -r "$dir/$tty.json" ]; then
    previous=$(/usr/bin/plutil -extract kind raw -o - "$dir/$tty.json" 2>/dev/null)
    case "$previous" in
        ask_user_question|permission_request|permission_prompt) exit 0 ;;
    esac
fi

# AskUserQuestion's input is the content: one to four complete questions and their options. Keep
# it as JSON rather than trying to quote arbitrary user text in sh. `head` places a hard ceiling
# on what can reach a note; an oversized input is omitted whole instead of leaving invalid JSON.
# The resulting note is always under 34 KiB even if Claude sends an unexpectedly huge payload.
tool_input=""
if [ "$kind" = "ask_user_question" ]; then
    tool_input=$(printf '%s' "$payload" \
        | /usr/bin/plutil -extract tool_input json -o - - 2>/dev/null \
        | /usr/bin/head -c 32769 2>/dev/null)
    input_bytes=$(printf '%s' "$tool_input" | /usr/bin/wc -c | /usr/bin/tr -d ' ')
    [ "$input_bytes" -le 32768 ] 2>/dev/null || tool_input=""
fi

# Written whole and moved into place, so a reader never sees half a line. The name is the tty,
# so a session has exactly one note and the newest one is the only one.
tmp="$dir/.$tty.$$"
{
    printf '{"event":"%s","kind":"%s","tty":"%s","at":%s' \
        "$event" "$kind" "$tty" "$(date +%s)"
    [ -n "$session" ] && printf ',"session":"%s"' "$session"
    [ -n "$tool_input" ] && printf ',"tool_input":%s' "$tool_input"
    printf '}\n'
} > "$tmp" 2>/dev/null || exit 0
mv -f "$tmp" "$dir/$tty.json" 2>/dev/null

# The session is over, so the thing that was remembered about it is now a file that will sit
# there until somebody sweeps it.
[ "$event" = "SessionEnd" ] && [ -n "$session" ] && rm -f "$dir/.tty-$session" 2>/dev/null

exit 0
