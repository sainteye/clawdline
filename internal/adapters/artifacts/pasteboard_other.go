//go:build !darwin

package artifacts

import "context"

// Pasteboard has nothing to drive outside macOS yet. Every method answers
// ErrPasteboardUnsupported, and the send path gives the assistant the path.
type Pasteboard struct{}

func NewPasteboard() Pasteboard { return Pasteboard{} }

func (Pasteboard) Available() bool { return false }

func (Pasteboard) Borrow(ctx context.Context) (*Borrowed, error) {
	return nil, ErrPasteboardUnsupported
}

func (Pasteboard) Offer(ctx context.Context, b *Borrowed, path string) error {
	return ErrPasteboardUnsupported
}

func (Pasteboard) GiveBack(ctx context.Context, b *Borrowed) error {
	return ErrPasteboardUnsupported
}
