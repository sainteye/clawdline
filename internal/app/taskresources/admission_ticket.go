package taskresources

import (
	"context"

	"github.com/google/uuid"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// AdmissionTicket binds a background measurement to this service incarnation,
// its resource revision and policy. It is server-owned, never client input.
type AdmissionTicket struct {
	Instance string              `json:"instance"`
	Revision uint64              `json:"revision"`
	Policy   taskresource.Policy `json:"policy"`
}

func (s *Service) AdmissionTicket() AdmissionTicket {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	if !s.stats.Known || taskresource.Admission(s.policy(), s.stats) != "" {
		return AdmissionTicket{}
	}
	if s.admissionInstance == "" {
		s.admissionInstance = uuid.NewString()
	}
	return AdmissionTicket{s.admissionInstance, s.admissionRevision, s.policy()}
}

// ConsumeAdmission rechecks the filesystem reserve without another tree walk.
// New allocations, measurements, uses, policy changes and an earlier grant
// invalidate a ticket. A restart has a different incarnation.
func (s *Service) ConsumeAdmission(t AdmissionTicket) bool {
	if s == nil || s.FreeSpace == nil || t.Instance == "" {
		return false
	}
	// Do not wait for a slow removal/attachment under the short lease request.
	// Busy mutation is not a verified point at which to consume readiness.
	if !s.mu.TryLock() {
		return false
	}
	defer s.mu.Unlock()
	if s.FS == nil || s.FS.VerifyRoot() != nil {
		return false
	}
	free, err := s.FreeSpace()
	if err != nil {
		return false
	}
	s.statsMu.Lock()
	defer s.statsMu.Unlock()
	u := s.stats
	u.Available = free
	if t.Instance != s.admissionInstance || t.Revision != s.admissionRevision || t.Policy != s.policy() || !u.Known || taskresource.Admission(s.policy(), u) != "" {
		return false
	}
	s.stats.Known = false
	s.admissionRevision++
	return true
}

// Maintenance shares admission's serialization without holding a lease or a
// store write transaction. Callers yield it between existing resource pages.
func (s *Service) Maintenance(ctx context.Context, work func() error) error {
	if err := s.lockAdmission(ctx); err != nil {
		return err
	}
	defer s.unlockAdmission()
	return work()
}

// Preserve the existing single-owner exclusion, but let a cancelled waiter
// leave it without waiting for unrelated filesystem work to finish. The queue
// remains the caller's existing bounded lease or callback intent.
func (s *Service) lockAdmission(ctx context.Context) error {
	s.admitOnce.Do(func() { s.admitGate = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.admitGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			s.unlockAdmission()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) unlockAdmission() { <-s.admitGate }
