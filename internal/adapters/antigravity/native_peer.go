package antigravity

import (
	"context"
	"syscall"
)

// NativeUnixPeer comes only from connected local socket kernel credentials.
// PID is the connector's PID, not stdin's claimed helper PID. Root must inspect
// its birth with NativeProcessReader before constructing a Witness, and enforce
// its own UID policy. A credential PID alone is not a process-birth fence.
type NativeUnixPeer struct {
	PID      int
	UID      uint32
	UIDKnown bool
}

// ReadNativeUnixPeer borrows the FD only inside RawConn.Control, so concurrent
// Close cannot recycle it beneath getsockopt. There is no FD duplication or I/O,
// no retry and no goroutine. Only a connected AF_UNIX stream socket is admitted.
func ReadNativeUnixPeer(ctx context.Context, conn syscall.Conn) (NativeUnixPeer, error) {
	if err := nativeContext(ctx); err != nil {
		return NativeUnixPeer{}, err
	}
	if conn == nil {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_invalid", nil)
	}
	raw, err := conn.SyscallConn()
	if err != nil || raw == nil {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unreadable", nil)
	}
	var peer NativeUnixPeer
	var readErr error
	err = raw.Control(func(fd uintptr) {
		if readErr = nativeContext(ctx); readErr == nil {
			peer, readErr = readNativeUnixPeerFD(fd)
		}
	})
	if cancelled := nativeContext(ctx); cancelled != nil {
		return NativeUnixPeer{}, cancelled
	}
	if err != nil {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unreadable", nil)
	}
	if readErr != nil {
		return NativeUnixPeer{}, readErr
	}
	if peer.PID <= 0 {
		return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_invalid", nil)
	}
	return peer, nil
}
