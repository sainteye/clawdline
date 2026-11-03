package sessionfiles

import (
	"bytes"
	"context"
	"image"

	"github.com/sainteye/clawdline/internal/adapters/artifacts"
	"github.com/sainteye/clawdline/internal/adapters/documents"
)

// Thumbnail is derived only from a registered file, after bounded header
// validation. It is never an arbitrary image-path endpoint.
func (s *Store) Thumbnail(ctx context.Context, key, id string) (Chunk, error) {
	first, err := s.Read(ctx, key, id, 0, "")
	if err != nil {
		return Chunk{}, err
	}
	if first.MediaType != "image/png" && first.MediaType != "image/jpeg" && first.MediaType != "image/gif" {
		return Chunk{}, ErrInvalid
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(first.Data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > documents.MaximumImageSide || config.Height > documents.MaximumImageSide || int64(config.Width)*int64(config.Height) > documents.MaximumImagePixels {
		return Chunk{}, ErrInvalid
	}
	data := append([]byte{}, first.Data...)
	for int64(len(data)) < first.TotalBytes {
		next, e := s.Read(ctx, key, id, int64(len(data)), first.Revision)
		if e != nil {
			return Chunk{}, e
		}
		data = append(data, next.Data...)
	}
	thumb, err := artifacts.MakeThumbnail(data)
	if err != nil {
		return Chunk{}, ErrInvalid
	}
	if len(thumb.JPEG) > documents.MaximumVideoChunkBytes {
		return Chunk{}, ErrLarge
	}
	first.MediaType = "image/jpeg"
	first.Data = thumb.JPEG
	first.TotalBytes = int64(len(thumb.JPEG))
	return first, nil
}
