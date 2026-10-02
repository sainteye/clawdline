package work

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ReviewBoundaryTitle names the ordinary item document in which the owner of
// a reviewed Feature plan says a later revision stays inside the reviewed risk
// boundary. It is durable, versioned and attributed to the owning Session.
const ReviewBoundaryTitle = "Review boundary assessment"

type ReviewBoundary struct {
	NewRiskBoundary *bool  `json:"new_risk_boundary"`
	Reason          string `json:"reason"`
}

func strictReviewJSON(body string, target any) error {
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("one JSON object is required")
	}
	return nil
}

func ParseReviewBoundary(body string) (ReviewBoundary, error) {
	var b ReviewBoundary
	if err := strictReviewJSON(body, &b); err != nil {
		return b, err
	}
	if b.NewRiskBoundary == nil || strings.TrimSpace(b.Reason) == "" {
		return b, errors.New("new_risk_boundary and a reason are required")
	}
	return b, nil
}

// A plan rewrite is covered by an earlier review only when its owner records
// why the rewrite stays inside that review's risk boundary. A changed boundary
// requires a new independent review. The declaration must follow that plan.
func UnchangedReviewBoundary(documents []DocumentV2, lastReview, lastPlan int) bool {
	if lastReview < 0 || lastPlan <= lastReview {
		return false
	}
	for n := len(documents) - 1; n > lastPlan; n-- {
		d := documents[n]
		if d.Role != "other" || d.Title != ReviewBoundaryTitle {
			continue
		}
		b, err := ParseReviewBoundary(d.Body)
		return err == nil && !*b.NewRiskBoundary
	}
	return false
}
