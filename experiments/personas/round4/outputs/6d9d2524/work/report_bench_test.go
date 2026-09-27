package main

import "testing"

func BenchmarkBuildReportParallel(b *testing.B) {
	s := NewStore(300, 1000)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			BuildReport(s)
		}
	})
}
