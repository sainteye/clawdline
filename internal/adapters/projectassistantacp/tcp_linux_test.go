//go:build linux

package projectassistantacp

import (
	"strings"
	"testing"
)

func TestTCPProcTablesUseSocketInodesAndListenState(t *testing.T) {
	text := "  sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n0: 0100007F:10E1 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 42\n1: 0100007F:9999 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 42\n2: 0100007F:8888 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 99\n"
	rows, err := parseTCPProcTable(text, map[string]int{"42": 10})
	if err != nil || len(rows) != 1 || rows[0].pid != 10 || rows[0].address.String() != "127.0.0.1:4321" {
		t.Fatal(rows, err)
	}
	v6 := strings.ReplaceAll(text, "0100007F", "00000000000000000000000001000000")
	rows, err = parseTCPProcTable(v6, map[string]int{"42": 10})
	if err != nil || len(rows) != 1 || rows[0].address.String() != "[::1]:4321" {
		t.Fatal(rows, err)
	}
}
func TestTCPProcIdentityParsesTheFinalCommandParenthesis(t *testing.T) {
	fields := strings.Fields("S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 12345 20")
	identity, err := parseTCPProcIdentity("10 (name ) with spaces) "+strings.Join(fields, " "), 10)
	if err != nil || identity.pid != 10 || identity.parent != 1 || identity.birth != "12345" {
		t.Fatal(identity, err)
	}
	if _, err := parseTCPProcIdentity("11 (foreign) "+strings.Join(fields, " "), 10); err == nil {
		t.Fatal("wrong PID accepted")
	}
}
