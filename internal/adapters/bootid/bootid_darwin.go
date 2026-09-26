package bootid

import (
	"context"
	"os/exec"
	"time"
)

// read is `sysctl -n kern.bootsessionuuid`: a UUID the kernel mints at boot and
// never changes until the next one.
func read(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "kern.bootsessionuuid").Output()
	if err != nil {
		return "", err
	}
	return clean(out)
}
