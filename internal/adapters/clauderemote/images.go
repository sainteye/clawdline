package clauderemote

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (c *Client) SendRemoteImages(ctx context.Context, s session.Session, text string, images []string, check func() error) error {
	ctx, cancel := context.WithTimeout(ctx, RequestSecondsLimit*time.Second)
	defer cancel()
	if len(images) == 0 || len(images) > artifacts.ProductionPolicy.MaxImagesPerMessage {
		return terminal.Unsent{Why: "Remote Control image count is invalid"}
	}
	content := []any{}
	if text != "" {
		content = append(content, map[string]string{"type": "text", "text": text})
	}
	for _, image := range images {
		raw, ok := artifacts.DecodeDataURL(image)
		if !ok {
			return terminal.Unsent{Why: "Remote Control attachment is not a supported image"}
		}
		picture, err := artifacts.Normalize(ctx, raw, artifacts.ProductionPolicy)
		if err != nil {
			return terminal.Unsent{Why: "Remote Control image could not be normalized"}
		}
		content = append(content, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": picture.MediaType, "data": base64.StdEncoding.EncodeToString(picture.Data)}})
	}
	if !c.valid(ctx, s) {
		return terminal.Unsent{Why: "Remote Control worker identity changed before image submission"}
	}
	if len(content) == 0 {
		return errors.New("Remote Control has no message content")
	}
	return c.submit(ctx, s, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}}, check)
}
