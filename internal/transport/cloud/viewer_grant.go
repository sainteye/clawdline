package cloud

import (
	"context"
	"crypto/ed25519"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

// viewerGrant uses the Session content and command gates for Cloud Bots.
// The pin store supplies the principal and reader only after transport proof.
func (l *Link) viewerGrant(ctx context.Context, account, sender string, verifiedKey ed25519.PublicKey) (adaptercloud.AssistantViewerGrant, error) {
	if account == "" || account != l.identity.AccountID || l.pinned == nil {
		return adaptercloud.AssistantViewerGrant{}, adaptercloud.ErrViewerNotPinned
	}
	read := l.transcriptAuthority(ctx, sender, verifiedKey)
	if !read.AllowsContentRead() {
		return adaptercloud.AssistantViewerGrant{}, adaptercloud.ErrViewerNotPinned
	}
	grant, err := l.pinned.VerifiedProjectAssistantGrant(account, sender, verifiedKey)
	if err != nil {
		return adaptercloud.AssistantViewerGrant{}, err
	}
	if grant.Invited != read.Invited {
		return adaptercloud.AssistantViewerGrant{}, adaptercloud.ErrViewerNotPinned
	}
	write := l.authority(ctx, sender, verifiedKey, true)
	grant.Writable = grant.Writable && write.AllowsOperation()
	return grant, nil
}
