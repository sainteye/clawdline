//go:build darwin

package projectassistantacp

import (
	"context"
	"os"
	"testing"
)

func TestTCPLsofMachineFieldsKeepIPv4IPv6AndDescriptorBoundaries(t *testing.T) {
	rows, err := parseTCPLsof("p10\nf5\ntIPv4\nn*:1234\nf6\ntIPv6\nn[::1]:2345\np11\nf9\ntIPv6\nn*:3456\n")
	if err != nil || len(rows) != 3 || rows[0].address.String() != "0.0.0.0:1234" || rows[1].address.String() != "[::1]:2345" || rows[2].address.String() != "[::]:3456" || rows[2].pid != 11 {
		t.Fatal(rows, err)
	}
	for _, bad := range []string{"p10\nn127.0.0.1:1234\n", "p10\nf3\ntIPv4\nnhttps://not-a-bind\n", "p10\nf3\ntIPv4\nn127.0.0.1:0\n", "p10\nf3\ntIPv4\nn127.0.0.1:1234\np99\nn127.0.0.1:2222\n"} {
		if rows, err := parseTCPLsof(bad); err == nil || rows != nil {
			t.Fatal("malformed inventory became addresses", rows, err)
		}
	}
}
func TestDarwinTCPRootUsesKernelUniqueIDBeyondBirthTime(t *testing.T) {
	root, err := openTCPRoot(context.Background(), os.Getpid(), testLimits().CaptureBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer root.close()
	pin := root.(*darwinTCPRoot)
	identity := pin.identity
	pin.identity.unique = "another-kernel-unique-id"
	if _, err := pin.read(context.Background()); err == nil {
		t.Fatal("same PID/birth bypassed the kernel unique ID", identity)
	}
}
