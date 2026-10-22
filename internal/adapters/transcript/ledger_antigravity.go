package transcript

import "encoding/json"

// feedAntigravity reprojects a complete bounded generation. No native tool
// UUID was observed; counters belong to planner step_index, not tool callbacks
// or generic results. Repeated reads replace the projection, never add it.
func (s *LedgerState) feedAntigravity(path string) (FeedResult, error) {
	snapshot, err := readAntigravitySnapshot(path)
	if err != nil {
		return FeedResult{}, err
	}
	if snapshot.info().Usage == nil {
		return FeedResult{}, antigravityError("usage_unavailable")
	}
	if s.AntigravityRevision == snapshot.revision {
		return FeedResult{}, nil
	}
	restarted := s.AntigravityRevision != "" && !snapshot.matchesRevision(s.AntigravityRevision, s.Offset)
	next := LedgerState{Assistant: "antigravity", AntigravityRevision: snapshot.revision,
		Offset: int64(len(snapshot.data)), Restarts: s.Restarts, ClassificationVersion: LedgerClassificationVersion,
		DailyVersion: s.DailyVersion, DailyTimezone: s.DailyTimezone, DailyCutoff: s.DailyCutoff,
		DailyUntil: s.DailyUntil, DailyProject: s.DailyProject, DailyProjectKey: s.DailyProjectKey,
		DailyProjectVersion: s.DailyProjectVersion}
	if restarted {
		next.Restarts++
	}
	head := snapshot.data[:min(len(snapshot.data), fingerprintBytes)]
	next.HeadLen, next.HeadSum = int64(len(head)), digest(head, int64(len(head)))
	for _, row := range snapshot.rows {
		next.Lines++
		if row.Source != "MODEL" || row.Type != "PLANNER_RESPONSE" {
			continue
		}
		before := next.dailyMeasured()
		next.Calls++
		amount := Tokens{Input: float64(*row.Input), CacheRead: float64(*row.CacheRead), Output: float64(*row.Output)}
		// Native logs do not identify a model price or a full prompt
		// snapshot. Input attribution stays other and money stays unknown.
		next.Measured.add(amount)
		next.charge(Sizes{CategoryOther: 1}, nil, Tokens{Input: amount.Input, CacheRead: amount.CacheRead}, "")
		actions := Sizes{}
		for _, tool := range row.Tools {
			category := antigravityToolCategory(tool)
			actions[category]++
		}
		if waitOnly(actions) {
			next.WaitCalls++
		}
		next.charge(actionWeights(actions), nil, Tokens{Output: amount.Output}, "")
		stamp, _ := json.Marshal(row.CreatedAt)
		next.dailyRecord(object{"timestamp": stamp}, before)
	}
	read := int64(len(snapshot.data))
	*s = next
	return FeedResult{Read: read, Restarted: restarted}, nil
}

func antigravityToolCategory(tool antigravityTool) Category {
	switch tool.Name {
	case "run_command":
		args, ok := decodeObject(tool.Args)
		if !ok {
			return CategoryOther
		}
		command, ok := args.str("CommandLine")
		if !ok {
			return CategoryOther
		}
		return classifyCommand(command)
	case "edit_file":
		return CategoryImpl
	}
	return CategoryOther
}
