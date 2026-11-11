package main

import (
	"context"
	"testing"
)

// The owned hosts must still see a live Cloud context while they stop.
func TestOwnedHostsStopWhileTheCloudContextIsStillAlive(t *testing.T) {
	cloudCtx, stopCloud := context.WithCancel(context.Background())
	var order []string
	aliveWhileHostsStopped := false
	stopServing(func() {
		aliveWhileHostsStopped = cloudCtx.Err() == nil
		order = append(order, "hosts")
	}, func() {
		stopCloud()
		order = append(order, "cloud")
	}, func() {
		order = append(order, "codegraph")
	})
	if !aliveWhileHostsStopped {
		t.Fatal("the Cloud context was cancelled before the owned hosts stopped")
	}
	if len(order) != 3 || order[0] != "hosts" || order[1] != "cloud" || order[2] != "codegraph" {
		t.Fatalf("stop order = %v", order)
	}
}
