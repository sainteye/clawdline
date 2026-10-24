package cloudkeys

import (
	"crypto/rand"
	"errors"
	"testing"
)

func TestRevocationJournalFailureBoundaries(t *testing.T) {
	for _, stage := range []string{":write", ":file-sync", ":rename", ":dir-sync"} {
		for _, phase := range []string{"begin", "complete"} {
			t.Run(phase+stage, func(t *testing.T) {
				root := t.TempDir()
				f, err := Open(root)
				if err != nil {
					t.Fatal(err)
				}
				initial, err := f.LoadOrCreateContentKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				var intent RevocationIntent
				if phase == "complete" {
					intent, err = f.BeginRevocation("A", initial.Epoch)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.RotateContentKey(intent.Operation, intent.ExpectedEpoch, rand.Reader); err != nil {
						t.Fatal(err)
					}
				}
				f.fault = func(at string) error {
					if at == revocationFile+stage {
						return errors.New("injected journal persistence failure")
					}
					return nil
				}
				if phase == "begin" {
					_, err = f.BeginRevocation("A", initial.Epoch)
				} else {
					err = f.CompleteRevocation(intent)
				}
				if err == nil {
					t.Fatal("fault was not exercised")
				}
				reopened, err := Open(root)
				if err != nil {
					t.Fatal(err)
				}
				got, pending, err := reopened.PendingRevocation()
				if err != nil {
					t.Fatal(err)
				}
				if phase == "begin" && !pending {
					got, err = reopened.BeginRevocation("A", initial.Epoch)
					if err != nil {
						t.Fatal(err)
					}
				}
				if phase == "complete" && !pending {
					return
				}
				first, err := reopened.RotateContentKey(got.Operation, got.ExpectedEpoch, rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				again, err := reopened.RotateContentKey(got.Operation, got.ExpectedEpoch, rand.Reader)
				if err != nil || first.KeyID != again.KeyID {
					t.Fatal("retry reminted", err)
				}
				if err := reopened.CompleteRevocation(got); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
