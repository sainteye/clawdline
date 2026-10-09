#!/bin/sh
# The compile itself, for tools/release/tmux/build.sh: on the Mac for darwin,
# inside the pinned Alpine image for linux. <work> holds src/ with the checked
# tarballs; the result goes to <work>/out.
#
# usage (by build.sh only): inside.sh <work>
set -eu

work=$1
src=$work/src
build=$work/build
prefix=$work/prefix
out=$work/out
# A container runs as root, so what it writes to the mounted <work> is
# root's; on a Linux runner build.sh then cannot remove it ("rm: cannot
# remove …/out/tmux: Permission denied", the first CI run on 2026-10-09).
# Hand it back to the caller on the way out, whether or not the build worked.
if [ -n "${HOST_UID:-}" ]; then
  trap 'chown -R "$HOST_UID:${HOST_GID:-$HOST_UID}" "$work" 2>/dev/null || true' EXIT
fi
if [ "$TARGET_OS" = linux ]; then
  # Compiled inside the container, not on the mounted <work>: from a Mac,
  # that mount is a case-insensitive filesystem, where ncurses's fallback
  # step writes `eterm` and `Eterm` to one file and stops ("name redefined").
  build=/tmp/clawdline-tmux/build
  prefix=/tmp/clawdline-tmux/prefix
fi
mkdir -p "$build" "$prefix/lib" "$prefix/include" "$out/licenses"
jobs=$( (getconf _NPROCESSORS_ONLN || sysctl -n hw.ncpu || echo 2) 2>/dev/null | head -1)

case "$TARGET_OS" in
  linux)
    # Build tools only; nothing from these packages is linked into tmux
    # except musl itself, which the image pins. ncurses here provides `tic`
    # and the terminfo sources the compiled-in fallback entries come from.
    apk add --no-cache build-base bison pkgconf ncurses ncurses-terminfo >/dev/null
    CC=cc
    CFLAGS="-O2 -fPIC"
    ;;
  darwin)
    CC="cc -arch $TARGET_ARCH"
    CFLAGS="-O2 -mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET:-13.0}"
    # A cross build names its host; the arm64 spelling is aarch64, which the
    # config.sub libevent 2.1.12 ships knows and arm64 is not.
    if [ "$TARGET_ARCH" != "$(uname -m)" ]; then
      case "$TARGET_ARCH" in arm64) host=aarch64-apple-darwin ;; *) host=$TARGET_ARCH-apple-darwin ;; esac
    fi
    ;;
  *) echo "inside.sh: TARGET_OS is linux or darwin" >&2; exit 2 ;;
esac
export CC CFLAGS

hostflag=""
[ -z "${host:-}" ] || hostflag="--host=$host"

cd "$build"
for t in "$src"/*.tar.gz; do tar -xzf "$t"; done

# utf8proc: one C file; its Makefile builds the static library.
cd "$build/utf8proc-$UTF8PROC_VERSION"
make -s CC="$CC" CFLAGS="$CFLAGS" libutf8proc.a
cp libutf8proc.a "$prefix/lib/"
cp utf8proc.h "$prefix/include/"
cp LICENSE.md "$out/licenses/utf8proc.txt"

# libevent: static, without OpenSSL, which tmux does not use.
cd "$build/libevent-$LIBEVENT_VERSION"
# shellcheck disable=SC2086
./configure -q --prefix="$prefix" $hostflag --disable-shared --enable-static \
  --disable-openssl --disable-samples --disable-libevent-regress --disable-debug-mode
make -s -j"$jobs" install >/dev/null
cp LICENSE "$out/licenses/libevent.txt"

ncurses_flags=""
if [ "$TARGET_OS" = linux ]; then
  # ncurses, static and wide. It reads whichever terminfo database the
  # distribution has, in the places distributions put it, and carries the
  # entries a terminal and tmux itself most need compiled in, for a machine
  # (a minimal container) that has no database at all.
  cd "$build/ncurses-$NCURSES_VERSION"
  ./configure -q --prefix="$prefix" --without-shared --with-normal --without-debug \
    --without-ada --without-cxx --without-cxx-binding --without-manpages --without-progs \
    --without-tests --enable-widec --disable-db-install \
    --with-terminfo-dirs=/etc/terminfo:/lib/terminfo:/usr/share/terminfo:/usr/lib/terminfo \
    --with-default-terminfo-dir=/usr/share/terminfo \
    --with-fallbacks=xterm-256color,xterm,screen-256color,screen,tmux-256color,tmux,vt100,linux
  make -s -j"$jobs" >/dev/null
  make -s install.libs install.includes >/dev/null
  # tmux's configure finds no ncurses.pc, falls back to -lncursesw, and on
  # seeing ncurses.h appends -lncurses as well; with no such library every
  # later link test fails, and it stops at "utf8proc not found". The name
  # is the same wide, static library.
  ln -sf libncursesw.a "$prefix/lib/libncurses.a"
  cp COPYING "$out/licenses/ncurses.txt"
  ncurses_flags="-I$prefix/include/ncursesw"
fi

# tmux. TERM inside a pane is screen-256color: every macOS and every Linux
# distribution has that entry, where tmux-256color is missing from older
# ones.
cd "$build/tmux-$TMUX_VERSION"
static=""
[ "$TARGET_OS" != linux ] || static="--enable-static"
PKG_CONFIG_PATH="$prefix/lib/pkgconfig" PKG_CONFIG_LIBDIR="$prefix/lib/pkgconfig" \
  CPPFLAGS="-I$prefix/include $ncurses_flags" LDFLAGS="-L$prefix/lib" \
  ./configure -q $hostflag $static --enable-utf8proc --with-TERM=screen-256color \
    LIBEVENT_CFLAGS="-I$prefix/include" LIBEVENT_LIBS="-L$prefix/lib -levent_core" \
    LIBEVENT_CORE_CFLAGS="-I$prefix/include" LIBEVENT_CORE_LIBS="-L$prefix/lib -levent_core" \
    LIBUTF8PROC_CFLAGS="-I$prefix/include" LIBUTF8PROC_LIBS="-L$prefix/lib -lutf8proc"
make -s -j"$jobs" >/dev/null
strip tmux
cp tmux "$out/tmux"
cp COPYING "$out/licenses/tmux.txt"
# A cross-built binary may not run here (an Intel one without Rosetta).
"$out/tmux" -V || echo "inside.sh: built for $TARGET_OS ${TARGET_ARCH:-}; it does not run on this machine"
