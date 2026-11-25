package projectassistant

import "testing"

// The diagnostics readings for the peer note and room picture bounds count
// what each check admitted and refused, so a refused value never raises the
// largest admitted one.
func TestBoundUsageCountsAdmittedAndRefused(t *testing.T) {
	var u boundUsage
	if !u.admit(2, 3) || !u.admit(3, 3) || !u.admit(1, 3) {
		t.Fatal("a value within the limit was refused")
	}
	if u.admit(4, 3) {
		t.Fatal("a value past the limit was admitted")
	}
	if got := u.usage(); got != (BoundUsage{Largest: 3, Refused: 1}) {
		t.Fatalf("usage = %+v, want largest 3 and one refusal", got)
	}
	before := RoomPictureUsage()
	if RoomPicturesFit(MaxRoomPictures + 1) {
		t.Fatal("a fifth picture fit")
	}
	if got := RoomPictureUsage(); got.Refused != before.Refused+1 {
		t.Fatalf("room picture refusals = %d, want %d", got.Refused, before.Refused+1)
	}
}
