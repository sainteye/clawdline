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
# No tty, no note. There is nothing to key it on, and a note nobody can match to a session on
# screen is worse than none: the app would show it as installed and working while telling you
# nothing. Screen reading covers this case, as it covers every case.
[ -n "$tty" ] || exit 0

# Written whole and moved into place, so a reader never sees half a line. The name is the tty,
# so a session has exactly one note and the newest one is the only one.
tmp="$dir/.$tty.$$"
{
    printf '{"event":"%s","tty":"%s","at":%s' "$event" "$tty" "$(date +%s)"
    [ -n "$session" ] && printf ',"session":"%s"' "$session"
    printf '}\n'
} > "$tmp" 2>/dev/null || exit 0
mv -f "$tmp" "$dir/$tty.json" 2>/dev/null

# The session is over, so the thing that was remembered about it is now a file that will sit
# there until somebody sweeps it.
[ "$event" = "SessionEnd" ] && [ -n "$session" ] && rm -f "$dir/.tty-$session" 2>/dev/null

exit 0
