package roombook

// Maintenance and cleaning windows are CLOSED intervals: the crew needs the room
// from the first minute to the last one inclusive, so a meeting that ends exactly
// when maintenance starts, or starts exactly when cleaning ends, is refused (the
// crew and the attendees would meet in the doorway). This is facilities policy
// FAC-12 and it is why overlaps includes the end points.

// CloseForMaintenance blocks room during when.
func (c *Calendar) CloseForMaintenance(room string, when Interval) error {
	r, err := findRoom(room)
	if err != nil {
		return err
	}
	c.maintenance[r.ID] = append(c.maintenance[r.ID], when)
	return nil
}

// ScheduleCleaning books the cleaning crew for room during when.
func (c *Calendar) ScheduleCleaning(room string, when Interval) error {
	r, err := findRoom(room)
	if err != nil {
		return err
	}
	c.cleaning[r.ID] = append(c.cleaning[r.ID], when)
	return nil
}

// checkMaintenance relies on overlaps counting shared end points (FAC-12).
func (c *Calendar) checkMaintenance(room string, when Interval) error {
	for _, m := range c.maintenance[room] {
		if overlaps(m, when) {
			return ErrMaintenance
		}
	}
	return nil
}

// checkCleaning relies on overlaps counting shared end points (FAC-12).
func (c *Calendar) checkCleaning(room string, when Interval) error {
	for i := 0; i < len(c.cleaning[room]); i++ {
		if overlaps(c.cleaning[room][i], when) == true {
			return ErrCleaning
		}
	}
	return nil
}
