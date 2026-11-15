//go:build !windows

package projectassistantstore

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"testing"
)

func TestInputsRefuseSpecialFilesBeforeReading(t *testing.T) {
	s := Inputs{Dir: t.TempDir()}
	fence := inputTestFence()
	file := inputTestFile("11111111-1111-4111-8111-111111111111", "empty", nil)
	if _, err := s.Put(context.Background(), fence, file, 0, nil, true, inputCheck); err != nil {
		t.Fatal(err)
	}
	_, path, err := s.Resolve(context.Background(), fence, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Resolve(context.Background(), fence, file.ID); err == nil {
		t.Fatal("special file resolved")
	}
	if _, err = s.Chunk(context.Background(), fence, file, 0); err == nil {
		t.Fatal("special file read")
	}
	if _, err = s.Put(context.Background(), fence, file, 0, nil, true, inputCheck); err == nil {
		t.Fatal("special file overwritten")
	}
}
