package projectassistant

// RoomTurnIDs identifies transcript provenance, independently of native session
// identity and origin. Host notices without a room ticket remain in own chat.
// Old facts without provenance are deliberately not guessed from their text.
func RoomTurnIDs(turns []Turn) map[string]bool {
	ids := make(map[string]bool)
	for _, turn := range turns {
		if turn.Room != nil {
			ids[turn.ID] = true
		}
	}
	return ids
}
