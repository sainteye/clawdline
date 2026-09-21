package process

import (
	"testing"
	"time"
)

func TestTheListenerOnAPortIsReadFromEachSystemsOwnTable(t *testing.T) {
	if got := parseLsofPIDs("p67783\nf12\n"); len(got) != 1 || got[0] != 67783 {
		t.Fatalf("lsof: %v", got)
	}
	if got := parseLsofPIDs(""); len(got) != 0 {
		t.Fatalf("lsof with nothing listening: %v", got)
	}

	ss := `LISTEN 0 4096 127.0.0.1:7727 0.0.0.0:* users:(("clawdline",pid=4242,fd=3))`
	if pid, ok := parseSSListener(ss); !ok || pid != 4242 {
		t.Fatalf("ss: %d %v", pid, ok)
	}
	if _, ok := parseSSListener(`LISTEN 0 4096 127.0.0.1:7727 0.0.0.0:*`); ok {
		t.Fatal("ss without a users column named a pid")
	}

	netstat := "\r\nActive Connections\r\n\r\n  Proto  Local Address   Foreign Address  State      PID\r\n" +
		"  TCP    127.0.0.1:17727  0.0.0.0:0        LISTENING  11\r\n" +
		"  TCP    127.0.0.1:7727   127.0.0.1:50000  ESTABLISHED 12\r\n" +
		"  TCP    127.0.0.1:7727   0.0.0.0:0        LISTENING  4040\r\n"
	if pid, ok := parseNetstatListener(netstat, 7727); !ok || pid != 4040 {
		t.Fatalf("netstat: %d %v (a longer port that ends in the same digits, or a connection, is not the listener)", pid, ok)
	}
}

// The day of the month is space-padded in `lstart`, so a single-digit day
// shifts every column after it by one.
func TestAStartTimeIsReadByFieldNotByColumn(t *testing.T) {
	for _, line := range []string{
		"Mon Sep 21 03:15:22 2026 /Applications/X.app/Contents/MacOS/clawdline serve",
		"Tue Sep  1 03:15:22 2026 /Applications/X.app/Contents/MacOS/clawdline serve",
	} {
		at, command, ok := parsePSStart(line, time.UTC)
		if !ok {
			t.Fatalf("%q did not parse", line)
		}
		if at.Hour() != 3 || at.Minute() != 15 || at.Second() != 22 || at.Year() != 2026 {
			t.Fatalf("%q: %v", line, at)
		}
		if command != "/Applications/X.app/Contents/MacOS/clawdline serve" {
			t.Fatalf("%q: command %q", line, command)
		}
	}
	if _, _, ok := parsePSStart("", time.UTC); ok {
		t.Fatal("an empty line parsed")
	}
}
