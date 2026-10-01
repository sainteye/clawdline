package work

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// These ordinary item documents are durable, versioned and attributed to the
// owning Session. Keeping the assessment with the item makes a routine-risk
// exemption inspectable without expanding every Board and Cloud wire shape.
const (
	ReviewRiskTitle     = "Review risk assessment"
	ReviewBoundaryTitle = "Review boundary assessment"
)

type ReviewRisk struct {
	ProductionDeployment *bool  `json:"production_deployment"`
	AccessOrSecurity     *bool  `json:"access_or_security"`
	CrossDataTransaction *bool  `json:"cross_data_transaction"`
	IrreversibleEffect   *bool  `json:"irreversible_effect"`
	Reason               string `json:"reason"`
}

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

func ParseReviewRisk(body string) (ReviewRisk, error) {
	var r ReviewRisk
	if err := strictReviewJSON(body, &r); err != nil {
		return r, err
	}
	if r.ProductionDeployment == nil || r.AccessOrSecurity == nil || r.CrossDataTransaction == nil ||
		r.IrreversibleEffect == nil || strings.TrimSpace(r.Reason) == "" {
		return r, errors.New("all four risk decisions and a reason are required")
	}
	return r, nil
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

// RoutineReviewRisk fails closed for absent or malformed assessments. A
// required production deployment cannot be called routine by its maker.
func RoutineReviewRisk(item ItemV2, documents []DocumentV2) bool {
	if item.DeploymentPolicy == DeployRequired {
		return false
	}
	for n := len(documents) - 1; n >= 0; n-- {
		d := documents[n]
		// A later plan may change the scope that was classified. Its owner
		// must record a fresh assessment before using the routine exemption.
		if d.Role == DocumentPlan {
			return false
		}
		if d.Role != "other" || d.Title != ReviewRiskTitle {
			continue
		}
		r, err := ParseReviewRisk(d.Body)
		return err == nil && !*r.ProductionDeployment && !*r.AccessOrSecurity &&
			!*r.CrossDataTransaction && !*r.IrreversibleEffect
	}
	return false
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
