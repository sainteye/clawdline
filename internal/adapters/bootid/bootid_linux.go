package bootid

import (
	"context"
	"os"
)

// read is /proc/sys/kernel/random/boot_id: a UUID the kernel generates once
// per boot.
func read(context.Context) (string, error) {
	out, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return clean(out)
}
