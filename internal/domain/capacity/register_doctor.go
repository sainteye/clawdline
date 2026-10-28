package capacity

func doctorBounds() []Entry {
	return []Entry{
		{Name: "diagnostics.doctor_checks", Class: Buffer, Unit: Rows, Limit: 1, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
		{Name: "diagnostics.doctor_history_rows", Class: Journal, Unit: Rows, Limit: 24, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.HistoryRowsLimit"}},
		{Name: "diagnostics.doctor_history_bytes", Class: Buffer, Unit: Bytes, Limit: 32768, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.HistoryBytesLimit"}},
		{Name: "diagnostics.doctor_history_seconds", Class: Journal, Unit: Seconds, Limit: 259200, AtLimit: EvictOldest, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.HistorySecondsLimit"}},
		{Name: "diagnostics.doctor_version_seconds", Class: Cache, Unit: Seconds, Limit: 3, AtLimit: Expire, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.VersionSecondsLimit"}},
		{Name: "diagnostics.doctor_version_bytes", Class: Buffer, Unit: Bytes, Limit: 4096, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.VersionBytesLimit"}},
		{Name: "diagnostics.doctor_report_bytes", Class: Buffer, Unit: Bytes, Limit: 8192, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon, Sources: []string{"internal/adapters/doctor.ReportBytesLimit"}},
		{Name: "diagnostics.doctor_description_characters", Class: Buffer, Unit: Rows, Limit: 500, AtLimit: Refuse, Told: []Channel{Diagnostics, Sender}, EvictedBy: Daemon},
	}
}
