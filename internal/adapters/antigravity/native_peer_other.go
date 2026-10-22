//go:build !darwin && !linux

package antigravity

func readNativeUnixPeerFD(uintptr) (NativeUnixPeer, error) {
	return NativeUnixPeer{}, nativeIdentityError("antigravity_native_peer_unsupported", nil)
}
