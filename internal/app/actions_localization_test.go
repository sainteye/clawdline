package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
)

func TestPictureLimitRefusalHasFixedCopyOrigin(t *testing.T) {
	images := make([]string, artifacts.ProductionPolicy.MaxImagesPerMessage+1)
	_, err := (Actions{}).SendWithPictures(context.Background(), "unused", "", images)
	ref, ok := err.(Refusal)
	if !ok || ref.RawDetail || ref.Code != "bad_request" ||
		!strings.Contains(ref.Detail, "at most 6 pictures") {
		t.Fatalf("picture limit refusal = %#v", err)
	}
}
