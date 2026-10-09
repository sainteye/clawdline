# The tmux a release carries, and everything it is built from
# (docs/design-decisions.md D33). Each tarball is checked against the hash
# below before anything is unpacked; a mismatch stops the build.
#
# Sourced by tools/release/tmux/build.sh; not run on its own.

# tmux 3.6a: the version this repository's parser is measured against
# (tmux.go); 3.4 escapes the 0x01 separator (docs/linux.md §4.1). ISC.
TMUX_VERSION=3.6a
TMUX_URL=https://github.com/tmux/tmux/releases/download/3.6a/tmux-3.6a.tar.gz
TMUX_SHA256=b6d8d9c76585db8ef5fa00d4931902fa4b8cbe8166f528f44fc403961a3f3759

# libevent: tmux's event loop. Three-clause BSD.
LIBEVENT_VERSION=2.1.12-stable
LIBEVENT_URL=https://github.com/libevent/libevent/releases/download/release-2.1.12-stable/libevent-2.1.12-stable.tar.gz
LIBEVENT_SHA256=92e6de1be9ec176428fd2367677e61ceffc2ee1cb119035037a27d346b0403bb

# utf8proc: character widths. macOS's wcwidth(3) is wrong for much of what an
# assistant prints, and tmux's configure refuses to build there without a
# decision either way. MIT.
UTF8PROC_VERSION=2.10.0
UTF8PROC_URL=https://github.com/JuliaStrings/utf8proc/releases/download/v2.10.0/utf8proc-2.10.0.tar.gz
UTF8PROC_SHA256=276a37dc4d1dd24d7896826a579f4439d1e5fe33603add786bb083cab802e23e

# ncurses: Linux only. macOS has libncurses and a terminfo database in the
# base system on every machine, so the darwin build links the system one. On
# Linux it is linked statically, reads the distribution's terminfo database
# where there is one, and carries the common entries compiled in for a
# machine that has none. MIT-style (X11).
NCURSES_VERSION=6.5
NCURSES_URL=https://ftp.gnu.org/gnu/ncurses/ncurses-6.5.tar.gz
NCURSES_SHA256=136d91bc269a9a5785e5f9e980bc76ab57428f604ce3e5a5a90cebc767971cc6

# The Linux build runs in this image, pinned by digest, so the C library a
# static binary carries (musl) is the same on every run.
ALPINE_IMAGE=alpine:3.20
