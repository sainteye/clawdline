//go:build darwin

package antigravity

import "golang.org/x/sys/unix"

func readNativeUnixPeerFD(fd uintptr) (NativeUnixPeer, error) {
	local, e1 := unix.Getsockname(int(fd))
	remote, e2 := unix.Getpeername(int(fd))
	kind, e3 := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
	_, localUnix := local.(*unix.SockaddrUnix)
	_, remoteUnix := remote.(*unix.SockaddrUnix)
	if e1 != nil || e2 != nil || e3 != nil || !localUnix || !remoteUnix || kind != unix.SOCK_STREAM {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_invalid", nil)
	}
	// SDK sys/un.h: SOL_LOCAL/LOCAL_PEERPID returns pid_t; LOCAL_PEERCRED
	// returns xucred. x/sys supplies the generated constants and struct layout.
	pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	if err != nil {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unreadable", nil)
	}
	cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil || cred.Version != 0 { // XUCRED_VERSION in sys/ucred.h.
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unreadable", nil)
	}
	return NativeUnixPeer{PID: pid, UID: cred.Uid, UIDKnown: true}, nil
}
