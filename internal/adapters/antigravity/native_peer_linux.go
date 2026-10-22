//go:build linux

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
	cred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unreadable", nil)
	}
	return NativeUnixPeer{PID: int(cred.Pid), UID: cred.Uid, UIDKnown: true}, nil
}
