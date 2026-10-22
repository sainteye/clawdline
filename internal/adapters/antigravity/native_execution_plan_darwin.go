//go:build darwin

package antigravity

import (
	"bytes"
	"context"
	"encoding/binary"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// KERN_PROCARGS2=49 is the Darwin sys/sysctl.h ABI, absent from x/sys constants.
const darwinKernProcArgs2 = 49

// Darwin KERN_PROCARGS2 returns argc, saved executable path, NUL padding,
// exactly argc arguments, and potentially an environment tail. Decode only
// the arguments and clear the entire owned buffer before returning. No
// environment key/value is decoded, retained, hashed or exposed.
func parseDarwinExecutionPlan(data []byte, executable string) (nativePlanTokens, error) {
	if len(data) < 4 || len(data) > MaxNativeProcessFileBytes {
		return nativePlanTokens{}, planInvalid()
	}
	argc := int(binary.NativeEndian.Uint32(data[:4]))
	data = data[4:]
	end := bytes.IndexByte(data, 0)
	if argc <= 0 || argc > len(data) || end <= 0 || string(data[:end]) != executable {
		return nativePlanTokens{}, planInvalid()
	}
	data = data[end+1:]
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}
	remaining := argc
	malformed := false
	result, err := parseNativeArguments(func() ([]byte, bool) {
		if remaining == 0 {
			return nil, false
		}
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			malformed = true
			return nil, false
		}
		word := data[:end]
		data = data[end+1:]
		remaining--
		return word, true
	}, executable)
	if err != nil || malformed || remaining != 0 {
		return nativePlanTokens{}, planInvalid()
	}
	return result, nil
}

func darwinProcessArguments(pid int, buffer []byte, size *uintptr) error {
	mib := [3]int32{unix.CTL_KERN, darwinKernProcArgs2, int32(pid)}
	var pointer uintptr
	if len(buffer) != 0 {
		pointer = uintptr(unsafe.Pointer(&buffer[0]))
	}
	_, _, errno := syscall.Syscall6(unix.SYS_SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), pointer, uintptr(unsafe.Pointer(size)), 0, 0)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return nativeIdentityError("antigravity_native_plan_unreadable", nil)
	}
	return nil
}

func readNativeExecutionPlan(ctx context.Context, pid int, executable string) (nativePlanTokens, error) {
	if err := nativeContext(ctx); err != nil {
		return nativePlanTokens{}, err
	}
	// Query before allocating. Do not let a kernel-reported size allocate an
	// unbounded buffer and do not retry a changed/short result.
	var size uintptr
	if err := darwinProcessArguments(pid, nil, &size); err != nil {
		return nativePlanTokens{}, err
	}
	if size < 4 || size > MaxNativeProcessFileBytes {
		return nativePlanTokens{}, nativeIdentityError("antigravity_native_process_file_too_large", nil)
	}
	buffer := make([]byte, int(size))
	defer clear(buffer)
	if err := darwinProcessArguments(pid, buffer, &size); err != nil {
		return nativePlanTokens{}, err
	}
	if size < 4 || size > uintptr(len(buffer)) {
		return nativePlanTokens{}, planInvalid()
	}
	if err := nativeContext(ctx); err != nil {
		return nativePlanTokens{}, err
	}
	return parseDarwinExecutionPlan(buffer[:size], executable)
}
