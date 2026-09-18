package board

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// The progress projection, ported decision for decision from
// `Sources/ProjectBoardStore.swift:3509` (`progressObject`).
//
// It is the product's answer to "what state is this work actually in", and it
// is derived from recorded evidence every time it is read rather than stored as
// somebody's opinion. That is the rule plan.md keeps as item 6: delivered is
// not reviewed and reviewed is not landed, so nothing here may promote one into
// another. The branch order is load-bearing — an earlier branch wins — so the
// chain is transcribed in the same order rather than rearranged into something
// tidier.

var activeAttemptStates = map[string]bool{"queued": true, "spawning": true, "briefed": true}

var terminalAttemptStates = map[string]bool{
	"success": true, "failure": true, "timeout": true, "cancelled": true, "spawn_failed": true,
}

var itemLinkKinds = map[string]bool{"related": true, "blocks": true, "coordinates": true}

// Progress is what a card carries about where the work stands.
type Progress struct {
	State               string         `json:"state"`
	Label               string         `json:"label"`
	Reason              string         `json:"reason"`
	Group               string         `json:"group"`
	BasisCodes          []string       `json:"basisCodes"`
	WarningCodes        []string       `json:"warningCodes"`
	LifecycleApplicable bool           `json:"lifecycleApplicable"`
	Active              bool           `json:"active"`
	Historical          bool           `json:"historical"`
	AttemptCounts       map[string]int `json:"attemptCounts"`
	EvidenceCounts      map[string]int `json:"evidenceCounts"`
}

// eventOrder is the total order the store puts events in. A known timestamp
// sorts above every unknown one; unknown timestamps sort among themselves by a
// durable observation ordinal. This is deliberately not "whichever was written
// last": an ingestion clock is not source chronology.
type eventOrder struct {
	at      *float64
	ordinal int64
	stable  string
}

func (a eventOrder) less(b eventOrder) bool {
	switch {
	case a.at != nil && b.at != nil && *a.at != *b.at:
		return *a.at < *b.at
	case a.at == nil && b.at != nil:
		return true
	case a.at != nil && b.at == nil:
		return false
	}
	if a.ordinal != b.ordinal {
		return a.ordinal < b.ordinal
	}
	return a.stable < b.stable
}

func linkOrder(l StoredLink) eventOrder {
	return eventOrder{at: l.EventAt, ordinal: l.ordinal(), stable: "link:" + l.TargetID}
}

func evidenceOrder(e StoredEvidence) eventOrder {
	at := e.EventAt
	// A root attestation is trusted to carry its own time; anything else must
	// have observed one.
	if at == nil && e.Source != nil && *e.Source == "root_attestation" {
		value := e.At
		at = &value
	}
	return eventOrder{at: at, ordinal: e.ordinal(), stable: "evidence:" + e.SourceID}
}

// identifiableDeclaration is true for a span whose id is the derived
// `span-<64 hex>` form. A span that cannot be attributed to a declaration is
// not proof that anybody is working.
func identifiableDeclaration(s StoredSpan) bool {
	if len(s.ID) != 69 || !strings.HasPrefix(s.ID, "span-") {
		return false
	}
	return isHex(s.ID[5:])
}

func isHex(value string) bool {
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return len(value) > 0
}

func isSettledNoChange(l StoredLink) bool {
	return l.attemptState() == "success" &&
		l.LandingDisposition != nil && *l.LandingDisposition == "nothing_to_land"
}

func usesArtifactAcceptance(i StoredItem) bool {
	accepted := 0
	for _, a := range i.Artifacts {
		if a.ReferenceOnly != nil && *a.ReferenceOnly {
			continue
		}
		if a.Kind == "commit" {
			return false
		}
		accepted++
	}
	return i.Type == "task" && accepted > 0
}

func hasVerificationEvidence(i StoredItem) bool {
	scope := i.scopeRevision()
	if usesArtifactAcceptance(i) {
		if i.CurrentArtifactAcceptanceID == nil || i.CurrentVerificationSubject == nil {
			return false
		}
		for _, e := range i.Evidence {
			if e.ID == *i.CurrentArtifactAcceptanceID && e.Kind == "artifact_acceptance" &&
				e.Status == "passed" && e.Subject != nil &&
				*e.Subject == *i.CurrentVerificationSubject && e.scopeRevision() == scope {
				return true
			}
		}
		return false
	}
	if i.CurrentVerificationEvidenceID == nil || i.CurrentVerificationSubject == nil {
		return false
	}
	for _, e := range i.Evidence {
		if e.ID == *i.CurrentVerificationEvidenceID && e.Kind == "verification" &&
			e.Status == "passed" && e.Subject != nil &&
			*e.Subject == *i.CurrentVerificationSubject && e.scopeRevision() == scope {
			return true
		}
	}
	return false
}

func hasDeliveryEvidence(i StoredItem) bool {
	if usesArtifactAcceptance(i) {
		return hasVerificationEvidence(i)
	}
	if i.Type == "coordination" {
		return i.PendingHandoff == nil && hasVerificationEvidence(i)
	}
	if i.CurrentLandingEvidenceID == nil {
		return false
	}
	for _, e := range i.Evidence {
		if e.ID != *i.CurrentLandingEvidenceID || e.Kind != "landing" || e.Status != "passed" {
			continue
		}
		if !samePointer(e.Subject, i.CurrentVerificationSubject) {
			continue
		}
		if e.scopeRevision() == i.scopeRevision() {
			return true
		}
	}
	return false
}

func samePointer(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// currentChecklistEvidenceIsValid is the Swift app's
// `validateCurrentChecklistEvidence` read as a predicate: every required row
// passed, with trusted evidence attesting one current subject and scope.
func currentChecklistEvidenceIsValid(i StoredItem) bool {
	var required []StoredChecklistRow
	for _, row := range i.Checklist {
		if row.Required {
			required = append(required, row)
		}
	}
	for _, row := range required {
		if row.Status != "passed" || row.EvidenceID == nil {
			return false
		}
	}
	if len(required) == 0 {
		return true
	}
	if i.CurrentVerificationSubject == nil {
		return false
	}
	scope := i.scopeRevision()
	for _, row := range required {
		coherent := false
		for _, e := range i.Evidence {
			if e.ID == *row.EvidenceID && e.Status == "passed" &&
				e.Subject != nil && *e.Subject == *i.CurrentVerificationSubject &&
				e.scopeRevision() == scope &&
				(e.Kind == "verification" || e.Kind == "artifact_acceptance") {
				coherent = true
				break
			}
		}
		if !coherent {
			return false
		}
	}
	return true
}

// ProgressOf projects one item. `members` is every other item, needed only so
// an Epic can see whether one of its delivery lanes is active.
func ProgressOf(item StoredItem, members []StoredItem) Progress {
	var attempts []StoredLink
	for _, l := range item.Links {
		if l.Kind == "task" && l.source() == "broker" {
			attempts = append(attempts, l)
		}
	}

	var active, queued, succeeded, failed, canceled, unknownAttempts int
	for _, a := range attempts {
		state := a.attemptState()
		if activeAttemptStates[state] {
			active++
		}
		switch state {
		case "queued", "spawning":
			queued++
		case "success":
			succeeded++
		case "failure", "timeout", "spawn_failed":
			failed++
		case "cancelled":
			canceled++
		}
		if !activeAttemptStates[state] && !terminalAttemptStates[state] {
			unknownAttempts++
		}
	}

	var openFindings, blockingFindings, passedVerifications, failedVerifications int
	var landings []StoredEvidence
	var verificationSummaries, artifactAcceptances int
	var blockingFindingRows []StoredEvidence
	for _, e := range item.Evidence {
		switch e.Kind {
		case "finding":
			if !e.Resolved {
				openFindings++
				if e.Blocking {
					blockingFindings++
					blockingFindingRows = append(blockingFindingRows, e)
				}
			}
		case "verification":
			if e.Status == "passed" {
				passedVerifications++
			} else if e.Status == "failed" {
				failedVerifications++
			}
		case "landing":
			if e.Status == "passed" {
				landings = append(landings, e)
			}
		case "verification_summary":
			verificationSummaries++
		case "artifact_acceptance":
			artifactAcceptances++
		}
	}

	var latestLanding *StoredEvidence
	for index := range landings {
		if latestLanding == nil || evidenceOrder(*latestLanding).less(evidenceOrder(landings[index])) {
			latestLanding = &landings[index]
		}
	}

	// An attempt observed after the landing but carrying no source time cannot
	// be ordered against it. That is surfaced rather than resolved: it must not
	// silently erase an established landing.
	var uncertainAfterLanding []StoredLink
	if latestLanding != nil {
		for _, a := range attempts {
			if a.EventAt == nil && a.ordinal() > latestLanding.ordinal() {
				uncertainAfterLanding = append(uncertainAfterLanding, a)
			}
		}
	}

	roundAttempts := attempts
	if latestLanding != nil {
		roundAttempts = nil
		for _, a := range attempts {
			if isSettledNoChange(a) {
				continue
			}
			uncertain := false
			for _, u := range uncertainAfterLanding {
				if u.ID == a.ID {
					uncertain = true
					break
				}
			}
			if activeAttemptStates[a.attemptState()] ||
				evidenceOrder(*latestLanding).less(linkOrder(a)) || uncertain {
				roundAttempts = append(roundAttempts, a)
			}
		}
	}

	var roundActive []StoredLink
	for _, a := range roundAttempts {
		if activeAttemptStates[a.attemptState()] {
			roundActive = append(roundActive, a)
		}
	}
	// A queued attempt names the phase it will perform, not an observed one.
	roundPhases := map[string]bool{}
	for _, a := range roundActive {
		if a.attemptState() == "briefed" && a.Phase != nil {
			roundPhases[*a.Phase] = true
		}
	}

	var unresolvedSpans int
	var declaredActivity *StoredSpan
	activeSpanCount := 0
	for index, s := range item.Spans {
		identifiable := identifiableDeclaration(s)
		if s.EndedAt == nil && s.source() != "broker" && !identifiable {
			unresolvedSpans++
		}
		if s.EndedAt == nil && s.source() != "broker" && identifiable {
			if declaredActivity == nil || declaredActivity.StartedAt < s.StartedAt {
				declaredActivity = &item.Spans[index]
			}
		}
		if s.EndedAt == nil && (s.source() == "broker" || identifiable) {
			activeSpanCount++
		}
	}

	var latestRoundAttempt *StoredLink
	for index := range roundAttempts {
		if latestRoundAttempt == nil ||
			linkOrder(*latestRoundAttempt).less(linkOrder(roundAttempts[index])) {
			latestRoundAttempt = &roundAttempts[index]
		}
	}
	roundSucceeded := latestRoundAttempt != nil && latestRoundAttempt.attemptState() == "success"
	roundFailed := false
	if latestRoundAttempt != nil {
		state := latestRoundAttempt.attemptState()
		roundFailed = state == "failure" || state == "timeout" || state == "spawn_failed"
	}

	currentEvidenceVerified := hasVerificationEvidence(item) &&
		currentChecklistEvidenceIsValid(item) && blockingFindings == 0

	var currentVerification *StoredEvidence
	for index, e := range item.Evidence {
		if item.CurrentVerificationEvidenceID != nil && e.ID == *item.CurrentVerificationEvidenceID {
			currentVerification = &item.Evidence[index]
			break
		}
	}
	if currentVerification == nil && item.CurrentArtifactAcceptanceID != nil {
		for index, e := range item.Evidence {
			if e.ID == *item.CurrentArtifactAcceptanceID {
				currentVerification = &item.Evidence[index]
				break
			}
		}
	}
	currentVerified := false
	if currentEvidenceVerified && currentVerification != nil {
		currentVerified = true
		for _, a := range roundAttempts {
			if attemptOccurredAfter(a, *currentVerification) {
				currentVerified = false
				break
			}
		}
	}

	var sessionDelivery *StoredSessionDelivery
	for index := range item.SessionDeliveries {
		d := &item.SessionDeliveries[index]
		if sessionDelivery == nil || sessionDelivery.At < d.At ||
			(sessionDelivery.At == d.At && sessionDelivery.EventID < d.EventID) {
			sessionDelivery = d
		}
	}
	currentSessionDelivery := currentDelivery(item, sessionDelivery, attempts)

	var latestVerification *StoredEvidence
	for index, e := range item.Evidence {
		if e.Kind != "verification" {
			continue
		}
		if latestVerification == nil ||
			evidenceOrder(*latestVerification).less(evidenceOrder(item.Evidence[index])) {
			latestVerification = &item.Evidence[index]
		}
	}
	failedProofAfterLanding := false
	if latestVerification != nil && latestVerification.Status == "failed" {
		failedProofAfterLanding = latestLanding == nil ||
			evidenceOrder(*latestLanding).less(evidenceOrder(*latestVerification))
	}
	blockingAfterLanding := blockingFindings > 0
	if latestLanding != nil {
		blockingAfterLanding = false
		for _, f := range blockingFindingRows {
			if evidenceOrder(*latestLanding).less(evidenceOrder(f)) {
				blockingAfterLanding = true
				break
			}
		}
	}
	scopeChangedAfterLanding := latestLanding != nil &&
		latestLanding.scopeRevision() < item.scopeRevision()

	isActive := active > 0 || activeSpanCount > 0

	warningCodes := warningsFor(item, latestLanding, unresolvedSpans, uncertainAfterLanding,
		blockingFindingRows)

	state, label, reason, basis := decide(item, decision{
		roundPhases: roundPhases, roundActive: roundActive, roundAttempts: roundAttempts,
		declaredActivity: declaredActivity, isActive: isActive, latestLanding: latestLanding,
		failedProofAfterLanding: failedProofAfterLanding, blockingAfterLanding: blockingAfterLanding,
		scopeChangedAfterLanding: scopeChangedAfterLanding, currentVerified: currentVerified,
		roundSucceeded: roundSucceeded, roundFailed: roundFailed, succeeded: succeeded,
		failed: failed, canceled: canceled, attempts: attempts, blockingFindings: blockingFindings,
		unresolvedSpans: unresolvedSpans, currentSessionDelivery: currentSessionDelivery,
	})

	// An Epic with an active member is active, whatever its own evidence says.
	if item.Type == "epic" && item.State != "canceled" {
		related := map[string]bool{}
		for _, l := range item.Links {
			if l.Kind == "related" {
				related[l.TargetID] = true
			}
		}
		for _, m := range members {
			if m.Type == "epic" {
				continue
			}
			isMember := (m.ParentID != nil && *m.ParentID == item.ID) || related[m.ID]
			if !isMember {
				continue
			}
			if ProgressOf(m, nil).Group == "active" {
				isActive = true
				state, label = "execution", "Delivery lanes active"
				reason = "Linked implementation items have current activity; each lane retains its own evidence."
				basis = []string{"active_delivery_lanes"}
				break
			}
		}
	}

	hasHistory := len(attempts) > 0 || len(item.Evidence) > 0 || sessionDelivery != nil

	// Activity and outcome are independent. A retained unresolved result is not
	// evidence that somebody is still working, and must not inflate the
	// project's active count.
	group := ""
	blockingObligation := false
	for _, o := range item.Obligations {
		if o.Blocking && !o.Resolved {
			blockingObligation = true
			break
		}
	}
	switch {
	case item.Type == "coordination":
		group = "coordination"
	case state == "landed" || state == "settled":
		group = "completed"
	case state == "canceled":
		group = "canceled"
	case isActive && state != "queued":
		group = "active"
	case state == "queued" || state == "verified" ||
		containsString(basis, "recorded_verification_incomplete_acceptance") || blockingObligation:
		group = "waiting"
	case hasHistory:
		group = "history"
	default:
		group = "waiting"
	}

	return Progress{
		State: state, Label: label, Reason: reason, Group: group,
		BasisCodes: basis, WarningCodes: warningCodes,
		LifecycleApplicable: item.Type != "coordination",
		Active:              isActive,
		Historical:          !isActive && hasHistory,
		AttemptCounts: map[string]int{
			"total": len(attempts), "active": active, "succeeded": succeeded,
		},
		EvidenceCounts: map[string]int{
			"blockingFindings": blockingFindings, "failedVerifications": failedVerifications,
			"landings": len(landings),
		},
	}
}

func attemptOccurredAfter(a StoredLink, e StoredEvidence) bool {
	if evidenceOrder(e).less(linkOrder(a)) {
		return true
	}
	return a.EventAt == nil && a.ordinal() > e.ordinal()
}

func currentDelivery(item StoredItem, delivery *StoredSessionDelivery,
	attempts []StoredLink) *StoredSessionDelivery {
	if delivery == nil || delivery.ScopeRevision != item.scopeRevision() {
		return nil
	}
	want := declaredSpanID("workflow:"+delivery.Provider+":"+delivery.SessionID, delivery.StartRequestID)
	var own *StoredSpan
	for index, s := range item.Spans {
		if s.ID == want && s.SessionID == delivery.SessionID && s.source() != "broker" {
			own = &item.Spans[index]
			break
		}
	}
	if own == nil {
		return nil
	}
	for _, s := range item.Spans {
		if s.ID != own.ID && s.source() != "broker" && identifiableDeclaration(s) &&
			s.StartedAt >= own.StartedAt {
			return nil
		}
	}
	for _, a := range attempts {
		if a.EventAt != nil && *a.EventAt > delivery.At {
			return nil
		}
	}
	return delivery
}

func warningsFor(item StoredItem, latestLanding *StoredEvidence, unresolvedSpans int,
	uncertainAfterLanding []StoredLink, blockingFindingRows []StoredEvidence) []string {
	codes := []string{}
	if hasVerificationEvidence(item) && !currentChecklistEvidenceIsValid(item) {
		codes = append(codes, "checklist_evidence_incomplete")
	}
	if unresolvedSpans > 0 {
		codes = append(codes, "legacy_span_identity_unresolved")
	}
	if len(uncertainAfterLanding) > 0 {
		codes = append(codes, "attempt_chronology_unresolved")
	}
	if latestLanding == nil {
		return codes
	}
	if !hasVerificationEvidence(item) {
		codes = append(codes, "exact_verification_missing")
	}
	if !containsString(codes, "checklist_evidence_incomplete") {
		hasRequired := false
		for _, row := range item.Checklist {
			if row.Required {
				hasRequired = true
				break
			}
		}
		if hasRequired && !currentChecklistEvidenceIsValid(item) {
			codes = append(codes, "checklist_evidence_incomplete")
		}
	}
	for _, f := range blockingFindingRows {
		if !evidenceOrder(*latestLanding).less(evidenceOrder(f)) {
			codes = append(codes, "historical_findings_unresolved")
			break
		}
	}
	for _, m := range item.Milestones {
		if m.Status != "passed" && m.Status != "not_applicable" {
			codes = append(codes, "milestones_incomplete")
			break
		}
	}
	for _, o := range item.Obligations {
		if !o.Resolved {
			codes = append(codes, "obligations_unresolved")
			break
		}
	}
	if item.PendingHandoff != nil {
		codes = append(codes, "handoff_pending")
	}
	return codes
}

type decision struct {
	roundPhases              map[string]bool
	roundActive              []StoredLink
	roundAttempts            []StoredLink
	declaredActivity         *StoredSpan
	isActive                 bool
	latestLanding            *StoredEvidence
	failedProofAfterLanding  bool
	blockingAfterLanding     bool
	scopeChangedAfterLanding bool
	currentVerified          bool
	roundSucceeded           bool
	roundFailed              bool
	succeeded                int
	failed                   int
	canceled                 int
	attempts                 []StoredLink
	blockingFindings         int
	unresolvedSpans          int
	currentSessionDelivery   *StoredSessionDelivery
}

// decide is the branch chain in the order the Swift app evaluates it. The order
// is the rule: an earlier branch wins, and rearranging them would change which
// evidence beats which.
func decide(item StoredItem, d decision) (state, label, reason string, basis []string) {
	allQueued := func(rows []StoredLink) bool {
		if len(rows) == 0 {
			return false
		}
		for _, a := range rows {
			if a.attemptState() != "queued" && a.attemptState() != "spawning" {
				return false
			}
		}
		return true
	}

	switch {
	case item.Type == "coordination":
		if allQueued(d.roundActive) {
			return "queued", "Queued",
				"Coordination is waiting to begin; lifecycle does not apply.",
				[]string{"coordination_context", "queued_attempt"}
		}
		if d.isActive {
			return "execution", "Active",
				"Coordination has an active attempt or declared interval; lifecycle does not apply.",
				[]string{"coordination_context", "active_interval"}
		}
		return "unknown", "Coordination",
			"Coordination is described by its period, handoff, relations, and outcomes rather than lifecycle status.",
			[]string{"coordination_context", "lifecycle_not_applicable"}

	case item.State == "canceled":
		return "canceled", "Canceled", "The work item was explicitly canceled.",
			[]string{"item_canceled"}

	case d.roundPhases["correction"]:
		return "correction", "Correction", "A linked broker attempt is correcting the work.",
			[]string{"active_correction_attempt"}

	case d.roundPhases["review_testing"]:
		return "review_testing", "Review & testing",
			"A linked broker review or test attempt is active.",
			[]string{"active_review_testing_attempt"}

	case d.roundPhases["planning"]:
		return "planning", "Planning", "A linked planning attempt is active.",
			[]string{"active_planning_attempt"}

	case d.declaredActivity != nil && noneBriefed(d.roundActive):
		// A declared interval is activity information, never a broker-observed
		// attempt: yesterday's successful discovery cannot make today's
		// declared implementation look delivered.
		phase := d.declaredActivity.Phase
		declared := "execution"
		switch phase {
		case "planning", "review_testing", "correction":
			declared = phase
		}
		return declared, "Declared activity",
			"A Session declared an active " + phase +
				" interval; broker execution is not established by this declaration.",
			[]string{"declared_" + phase + "_span"}

	case allQueued(d.roundActive):
		return "queued", "Queued", "Linked broker attempts are waiting to execute.",
			[]string{"queued_attempt"}

	case len(d.roundActive) > 0:
		return "execution", "In progress", "A linked broker attempt is executing.",
			[]string{"active_execution_attempt"}

	case d.failedProofAfterLanding || d.blockingAfterLanding || d.scopeChangedAfterLanding:
		code := "scope_changed_after_landing"
		if d.failedProofAfterLanding {
			code = "verification_failed_after_landing"
		} else if d.blockingAfterLanding {
			code = "blocking_finding_after_landing"
		}
		if d.latestLanding == nil {
			open := "open_blocking_finding"
			if d.failedProofAfterLanding {
				open = "verification_failed"
			}
			return "blocked", "Blocked",
				"Current proof or a blocking finding requires correction.", []string{open}
		}
		return "correction", "Correction",
			"New evidence after the last landing opened another work round.",
			[]string{code, "historical_landing_retained"}

	case d.latestLanding != nil && len(d.roundAttempts) == 0:
		return "landed", "Landed",
			"Broker ancestry confirms the latest observed delivery landed.",
			landingBasis(d.latestLanding)

	case usesArtifactAcceptance(item) && d.currentVerified && hasDeliveryEvidence(item):
		return "landed", "Delivered",
			"An administrative user accepted the current non-code artifact.",
			[]string{"authoritative_artifact_acceptance"}

	case settledNoChange(item, d):
		return "settled", "Execution finished",
			"The root confirmed this execution needs no repository landing; this is not a Feature verification claim.",
			[]string{"root_settled_no_change_execution"}

	case d.currentVerified:
		return "verified", "Verified", "Current-scope trusted verification passed.",
			[]string{"current_exact_verification"}

	case d.roundSucceeded:
		code := "task_delivery_only"
		if d.latestLanding != nil {
			code = "new_delivery_after_landing"
		}
		return "delivered", "Delivered",
			"A child delivered output; integration is not yet proven.", []string{code}

	case d.roundFailed:
		if d.latestLanding == nil {
			return "blocked", "Blocked", "The retained attempts ended without delivery.",
				[]string{"terminal_attempt_failure"}
		}
		return "correction", "Correction",
			"A newer attempt ended unsuccessfully after the last landing.",
			[]string{"attempt_failure_after_landing", "historical_landing_retained"}

	case d.latestLanding != nil:
		return "landed", "Landed",
			"Broker ancestry confirms the latest observed delivery landed.",
			landingBasis(d.latestLanding)

	case d.succeeded > 0:
		return "delivered", "Delivered",
			"A child delivered output; integration is not yet proven.",
			[]string{"task_delivery_only"}

	case d.failed > 0:
		return "blocked", "Blocked", "The retained attempts ended without delivery.",
			[]string{"terminal_attempt_failure"}

	case d.canceled > 0 && d.canceled == len(d.attempts):
		return "canceled", "Canceled", "All retained attempts were canceled.",
			[]string{"all_attempts_canceled"}

	case hasVerificationEvidence(item) && !currentChecklistEvidenceIsValid(item):
		return "review_testing", "Acceptance incomplete",
			"Verification is recorded, but required checklist acceptance is incomplete; no active test run is implied.",
			[]string{"recorded_verification_incomplete_acceptance"}

	case d.currentSessionDelivery != nil:
		delivery := d.currentSessionDelivery
		if delivery.Disposition == "delivered" {
			if delivery.Phase == "planning" {
				return "planning", "Planning delivered",
					"The Session delivered planning work; implementation is not established.",
					[]string{"session_planning_delivery"}
			}
			return "delivered", "Session delivered",
				"The Session reported delivery; independent verification, landing and release are separate.",
				[]string{"assistant_attested_delivery"}
		}
		return "unknown", "Session follow-up",
			"The Session ended with " + delivery.Disposition + "; no completion is implied.",
			[]string{"session_" + delivery.Disposition}

	case d.unresolvedSpans > 0:
		return "unknown", "Activity unconfirmed",
			"A retained interval has no verifiable declaration identity; it is not proof of current activity or completion.",
			[]string{"legacy_span_identity_unresolved"}

	case item.State == "planning" || item.State == "backlog":
		code := "item_planning"
		if item.State == "backlog" {
			code = "item_backlog"
		}
		return "planning", "Planning", "The item has not begun an execution attempt.",
			[]string{code}
	}

	return "unknown", "Unknown", "Retained facts do not establish a current progress state.",
		[]string{"insufficient_evidence"}
}

func settledNoChange(item StoredItem, d decision) bool {
	if item.InferredSourceKey == nil || item.Type != "task" || len(d.attempts) == 0 {
		return false
	}
	for _, a := range d.attempts {
		if !isSettledNoChange(a) {
			return false
		}
	}
	if d.isActive || d.blockingFindings > 0 {
		return false
	}
	for _, o := range item.Obligations {
		if o.Blocking && !o.Resolved {
			return false
		}
	}
	return true
}

func noneBriefed(rows []StoredLink) bool {
	for _, a := range rows {
		if a.attemptState() == "briefed" {
			return false
		}
	}
	return true
}

func landingBasis(landing *StoredEvidence) []string {
	return []string{"authoritative_broker_landing",
		"landing:" + strconv.FormatInt(int64(landing.At), 10)}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// ListSummary is the bounded attention fact a list row carries. It never
// carries the raw obligation or evidence arrays.
type ListSummary struct {
	Group     string         `json:"group"`
	Coverage  string         `json:"coverage"`
	Attention map[string]int `json:"attention"`
	View      AudienceView   `json:"view"`
}

// AudienceView is who a row is for and what part it plays.
type AudienceView struct {
	Audience       string   `json:"audience"`
	Role           string   `json:"role"`
	DefaultVisible bool     `json:"defaultVisible"`
	ReasonCodes    []string `json:"reasonCodes"`
	Outcome        string   `json:"outcome,omitempty"`
	AuditID        string   `json:"auditId,omitempty"`
	ParentID       *string  `json:"-"`
	// parentKnown is true for a reconciled row, whose parentId key is always
	// present and may be null; an unreconciled row carries no parentId key.
	parentKnown bool
}

// MarshalJSON keeps the one key whose absence and nullness mean different
// things: a reconciled row says `parentId: null`, an unreconciled one says
// nothing.
func (v AudienceView) MarshalJSON() ([]byte, error) {
	type plain AudienceView
	body, err := json.Marshal(plain(v))
	if err != nil || !v.parentKnown {
		return body, err
	}
	parent, err := json.Marshal(v.ParentID)
	if err != nil {
		return nil, err
	}
	return append(append(body[:len(body)-1], []byte(`,"parentId":`)...), append(parent, '}')...), nil
}

// ListSummaryOf is `ProjectBoardStore.listSummary`. The display group is not
// always the progress group: a blocked item or one with anything demanding
// attention is shown as waiting, and required future checklist scope is not a
// present blocker, so that case is shown as planning.
func ListSummaryOf(item StoredItem, progress Progress) ListSummary {
	blocking, decisions := 0, 0
	for _, o := range item.Obligations {
		if o.Resolved {
			continue
		}
		if o.Blocking {
			blocking++
		}
		if o.ActorKind != nil && *o.ActorKind == "user" {
			decisions++
		}
	}
	attention := map[string]int{
		"blockingObligations": blocking,
		"userDecisions":       decisions,
		"blockingFindings":    progress.EvidenceCounts["blockingFindings"],
		"failedVerifications": progress.EvidenceCounts["failedVerifications"],
	}
	anyAttention := false
	for _, v := range attention {
		if v > 0 {
			anyAttention = true
			break
		}
	}

	group := progress.Group
	phase := progress.State
	display := group
	switch {
	case group == "coordination" || group == "completed" || group == "canceled" || group == "active":
		display = group
	case item.State == "blocked" || phase == "blocked" || anyAttention:
		display = "waiting"
	case group == "waiting" && phase == "planning":
		display = "planning"
	}

	return ListSummary{Group: display, Coverage: "complete", Attention: attention,
		View: audienceView(item)}
}

// audienceView is `ProjectBoardStore.audienceView`. A reconciled row says what
// the reconciliation decided. An unreconciled row is classified by its inferred
// source identity alone: titles such as "Review" and lifecycle states are
// deliberately not inputs, because either can also describe work a person
// explicitly created.
func audienceView(item StoredItem) AudienceView {
	if d := item.CatalogDisposition; d != nil {
		return AudienceView{
			Audience: d.Audience, Role: d.Role, DefaultVisible: d.Audience == "human",
			ReasonCodes: []string{"catalog_reconciled"}, Outcome: d.Outcome,
			AuditID: d.AuditID, ParentID: d.ParentID, parentKnown: true,
		}
	}
	if item.InferredSourceKey != nil {
		execution := false
		for _, l := range item.Links {
			if l.source() == "broker" && (l.Kind == "task" || l.SourceTaskID != nil) {
				execution = true
				break
			}
		}
		for _, s := range item.Spans {
			if s.source() == "broker" {
				execution = true
			}
		}
		for _, e := range item.Evidence {
			if e.Source != nil && *e.Source == "broker" {
				execution = true
			}
		}
		if execution {
			return AudienceView{Audience: "agent", Role: "execution_record",
				ReasonCodes: []string{"inferred_broker_record"}}
		}
		return AudienceView{Audience: "agent", Role: "provenance_record",
			ReasonCodes: []string{"inferred_provenance"}}
	}
	if item.ParentID == nil {
		return AudienceView{Audience: "human", Role: "primary_work", DefaultVisible: true,
			ReasonCodes: []string{"explicit_work_item"}}
	}
	return AudienceView{Audience: "human", Role: "subtask", DefaultVisible: true,
		ReasonCodes: []string{"explicit_parent"}}
}

// CardSummary is the exact checklist and milestone count. Nothing here is
// estimated: a row is complete when its status says so.
type CardSummary struct {
	Checklist  map[string]any `json:"checklist"`
	Milestones map[string]any `json:"milestones"`
}

// CardSummaryOf counts the item's acceptance rows.
func CardSummaryOf(item StoredItem) CardSummary {
	complete := map[string]bool{"passed": true, "not_applicable": true}
	checklistDone, required, requiredDone := 0, 0, 0
	for _, row := range item.Checklist {
		if complete[row.Status] {
			checklistDone++
		}
		if row.Required {
			required++
			if complete[row.Status] {
				requiredDone++
			}
		}
	}
	milestonesDone := 0
	for _, m := range item.Milestones {
		if complete[m.Status] {
			milestonesDone++
		}
	}
	return CardSummary{
		Checklist: map[string]any{
			"total": len(item.Checklist), "completed": checklistDone,
			"required": required, "requiredCompleted": requiredDone, "coverage": "complete",
		},
		Milestones: map[string]any{
			"total": len(item.Milestones), "completed": milestonesDone, "coverage": "complete",
		},
	}
}

// sortItems is the store's order: newest first, ties broken by key so the same
// second does not shuffle between reads.
func sortItems(items []StoredItem) {
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].UpdatedAt == items[b].UpdatedAt {
			return items[a].Key < items[b].Key
		}
		return items[a].UpdatedAt > items[b].UpdatedAt
	})
}
