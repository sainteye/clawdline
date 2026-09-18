package orchestrator

// The inventory's own JSON, assembled once.
//
// This is the one payload on this daemon built as a map rather than as a
// generated type, and the reason is specific rather than convenient: the 409
// `stale_inventory` refusal carries **the whole 200 body** inside its error
// object, so that recovering from a stale receipt is one round trip. Those two
// have to be the same bytes. Two assemblers — one for the route, one for the
// refusal — is exactly the drift this repository's generator exists to prevent,
// and the generator cannot help here because the refusal's envelope is a
// heterogeneous `error` object rather than a named type.
//
// The three sections publish three different key sets on purpose, copied from
// the Swift app: `live` carries `assistant` and `overlaps` and no `head`;
// `unlanded` carries `head`, `landing` and `why` and no `assistant`; and
// `droppable` carries neither `claims` nor `state`. A reader that finds a key
// it did not expect is reading the wrong section, which is a better failure
// than one uniform shape where every absence means the same nothing.

// InventoryPayload is the body of GET /v1/orchestrator/inventory.
func InventoryPayload(inv Inventory) map[string]any {
	live := make([]map[string]any, 0, len(inv.Live))
	for _, row := range inv.Live {
		item := map[string]any{
			"task":        row.Task,
			"title":       row.Title,
			"state":       string(row.State),
			"assistant":   row.Assistant,
			"claims":      row.Claims,
			"age_seconds": row.Age,
			"do":          row.Do,
			"root_label":  nullable(row.RootLabel),
			"root_key":    nullable(row.RootKey),
			"branch":      nullable(row.Branch),
		}
		// `overlaps` appears only when the caller asked with `claims=`. An
		// empty array would say "nothing of yours is in the way", which is a
		// different answer from "you did not ask".
		if row.Overlaps != nil {
			item["overlaps"] = row.Overlaps
		}
		live = append(live, item)
	}
	unlanded := make([]map[string]any, 0, len(inv.Unlanded))
	for _, row := range inv.Unlanded {
		unlanded = append(unlanded, map[string]any{
			"task":        row.Task,
			"title":       row.Title,
			"state":       string(row.State),
			"claims":      row.Claims,
			"age_seconds": row.Age,
			"do":          row.Do,
			"branch":      nullable(row.Branch),
			"head":        nullable(row.Head),
			"landing":     nullable(string(row.Landing)),
			"why":         nullable(row.Why),
		})
	}
	droppable := make([]map[string]any, 0, len(inv.Droppable))
	for _, row := range inv.Droppable {
		var path any
		if row.OnDisk {
			path = row.Path
		}
		droppable = append(droppable, map[string]any{
			"task":          row.Task,
			"title":         row.Title,
			"branch":        nullable(row.Branch),
			"path":          path,
			"branch_exists": row.Branched,
			"why":           row.Why,
			"age_seconds":   row.Age,
			"do":            row.Do,
		})
	}
	return map[string]any{
		"schema_version": inv.Schema,
		"repository":     inv.Repository,
		"generation":     inv.Generation,
		"live":           live,
		"unlanded":       unlanded,
		"droppable":      droppable,
		"digest": map[string]any{
			"sealed":   DigestSealed,
			"excluded": DigestExcluded,
		},
		"task_root": inv.TaskRoot,
		"at":        inv.At.Unix(),
	}
}

// nullable is how an unset string reaches the wire: present, and null.
//
// Absent and null are different promises. A client written against "the key is
// always there and may be null" breaks on a key that vanishes, and the Swift
// console is written against the first.
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
