package taskresources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

func TestResourceLookupPreservesMissingCancellationDeadlineAndReadFailure(t *testing.T) {
	for _, name := range []string{"missing", "canceled", "deadline", "read failure"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, taskresource.Cache)
			ctx := context.Background()
			id := f.allocation.Resource.ID
			var cause error
			want := "resource_not_found"
			switch name {
			case "missing":
				id = uuid.NewString()
				cause = sql.ErrNoRows
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want, cause = "resource_lookup_canceled", context.Canceled
			case "deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
				want, cause = "resource_lookup_deadline_exceeded", context.DeadlineExceeded
			case "read failure":
				if err := f.s.Store.Close(); err != nil {
					t.Fatal(err)
				}
				want = "resource_read_failed"
			}
			ctx = WithCaller(ctx, "resources.inspect", "")
			_, err := f.s.Plan(ctx, f.scope, []string{id})
			var failure Failure
			if !errors.As(err, &failure) || failure.Code != want {
				t.Fatalf("%s classified as %v; want %s", name, err, want)
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatalf("underlying %v was lost: %v", cause, err)
			}
			diagnostic := failure.Diagnostic()
			if diagnostic.Code != want || diagnostic.Caller != "resources.inspect" || diagnostic.Phase != "resource.lookup" || diagnostic.ResourceID != id {
				t.Fatal("lookup correlation lost", diagnostic)
			}
			deadline, hasDeadline := ctx.Deadline()
			if diagnostic.HasDeadline != hasDeadline || hasDeadline && diagnostic.DeadlineUnixMilli != deadline.UnixMilli() {
				t.Fatal("deadline evidence lost", diagnostic)
			}
			preserved(t, f)
		})
	}
}

func TestResourceDiagnosticNeverSerializesErrorContentsOrCallerData(t *testing.T) {
	const private = "/private/account/source-data"
	cause := errors.New(private)
	ctx := WithCaller(context.Background(), private, private)
	failure := readFailure(ctx, cause, "resource.lookup", private)
	raw, err := json.Marshal(failure.Diagnostic())
	if err != nil || strings.Contains(string(raw), private) || strings.Contains(failure.Error(), private) || !errors.Is(failure, cause) {
		t.Fatal("diagnostic leaked content or discarded its cause", string(raw), err)
	}
	raw, err = json.Marshal(failure)
	if err != nil || strings.Contains(string(raw), private) {
		t.Fatal("error serialization exposed the retained cause", string(raw), err)
	}
}

func TestMeasurementReadFailureCannotBecomeAClaimOfMeasuredPressure(t *testing.T) {
	f := setup(t, taskresource.Cache)
	f.s.OtherUsage = func(context.Context) (taskresource.Usage, error) {
		return taskresource.Usage{}, fail("cache_total_over_budget")
	}
	_, err := f.s.Measure(context.Background())
	var failure Failure
	if !errors.As(err, &failure) || failure.Code != "resource_legacy_measurement_unknown" || f.s.Usage().Known {
		t.Fatal("an unreadable source became measured capacity pressure", err)
	}
	preserved(t, f)
}

func TestLegacyPreparationReasonIsPreservedButPlainMissingTextIsNotProof(t *testing.T) {
	for _, item := range []struct{ source, code string }{
		{"resource_measurement_unknown", "resource_measurement_unknown"},
		{"resource_identity_changed", "resource_identity_changed"},
		{"resource_not_found", "resource_read_failed"},
	} {
		cause := errors.New(item.source)
		failure := Explain(context.Background(), cause, "callback.prepare")
		if failure.Code != item.code || !errors.Is(failure, cause) || failure.Diagnostic().Phase != "callback.prepare" {
			t.Fatal(item, failure)
		}
	}
}
