package bootid

import (
	"context"
	"errors"
	"testing"
)

func TestCleanTakesOneLineAndRefusesNothing(t *testing.T) {
	if id, err := clean([]byte("  1F0A-22\n")); err != nil || id != "1F0A-22" {
		t.Fatalf("clean = %q, %v", id, err)
	}
	if _, err := clean([]byte(" \n")); !errors.Is(err, ErrEmpty) {
		t.Fatalf("an empty answer was not ErrEmpty: %v", err)
	}
	if _, err := clean([]byte("a\nb")); err == nil {
		t.Fatal("two lines were taken as one id")
	}
}

// TestReadAnswersTheSameIdTwice is the property the feature rests on: within
// one boot the id does not move. On a platform with no source it is an error,
// never an id.
func TestReadAnswersTheSameIdTwice(t *testing.T) {
	first, err := Read(context.Background())
	if err != nil {
		if !supported {
			return
		}
		t.Fatalf("this platform has a boot id and it could not be read: %v", err)
	}
	if !supported {
		t.Fatalf("an unsupported platform answered an id: %q", first)
	}
	second, err := Read(context.Background())
	if err != nil || second != first {
		t.Fatalf("the boot id moved inside one boot: %q then %q (%v)", first, second, err)
	}
}
