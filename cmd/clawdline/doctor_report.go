package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
)

// doctor --json asks the running daemon so its version and evidence, rather
// than a different CLI installation's state directory, name the subject.
func doctorJSON() {
	b, err := openBroker(0)
	if err != nil {
		fail(err)
		return
	}
	os.Exit(readDoctorReport(os.Stdout, os.Stderr, b))
}

func readDoctorReport(stdout, stderr io.Writer, b *broker) int {
	a, err := b.request(http.MethodGet, "/v1/doctor", nil, nil, "")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return report(stdout, stderr, "doctor", a)
}
