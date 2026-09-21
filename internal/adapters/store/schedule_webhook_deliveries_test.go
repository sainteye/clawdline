package store

import (
	"context"
	"errors"
	"testing"
)

func TestWebhookDeliveryReservationIsIdempotentAndRejectsDifferentBytes(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	row := ScheduleWebhookDelivery{Version: 1, DeliveryID: "swd_a", DeliveryDigest: "digest-a",
		HookID: "swh_a", State: "prepared", TaskID: "task-a", CreatedAt: 1, UpdatedAt: 1}
	got, created, err := st.ReserveScheduleWebhookDelivery(ctx, row)
	if err != nil || !created || got.TaskID != "task-a" {
		t.Fatalf("first reserve: %+v %v %v", got, created, err)
	}
	got, created, err = st.ReserveScheduleWebhookDelivery(ctx, row)
	if err != nil || created || got.TaskID != "task-a" {
		t.Fatalf("replay: %+v %v %v", got, created, err)
	}
	row.DeliveryDigest = "digest-b"
	if _, _, err := st.ReserveScheduleWebhookDelivery(ctx, row); !errors.Is(err, ErrWebhookDeliveryConflict) {
		t.Fatalf("different delivery bytes answered %v", err)
	}
}
