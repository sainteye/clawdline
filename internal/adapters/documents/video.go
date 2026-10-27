package documents

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

// MaximumVideoBytes bounds the complete browser Blob; MaximumVideoChunkBytes
// bounds each independently authorized answer. Text documents keep their cap.
const MaximumVideoBytes = 32 * 1024 * 1024
const MaximumVideoChunkBytes = 512 * 1024

const (
	VideoChanged Refusal = "video_changed"
	VideoInvalid Refusal = "video_format_unsupported"
	VideoRange   Refusal = "video_range_invalid"
)

// VideoChunk never contains a disk path. Revision is a size/mtime version,
// not a content hash or a defence against an authorized local writer preserving
// timestamps. OpenRoot prevents symlink replacement from escaping the root.
type VideoChunk struct {
	MediaType  string `json:"media_type"`
	TotalBytes int64  `json:"total_bytes"`
	Offset     int64  `json:"offset"`
	Revision   string `json:"revision"`
	ByteCount  int    `json:"byte_count"`
	Data       []byte `json:"data"`
}

func ReadVideo(ctx context.Context, root, name string, offset int64, revision string) (VideoChunk, error) {
	located, err := file(root, name, true)
	if err != nil {
		return VideoChunk{}, err
	}
	if offset < 0 || offset >= located.Bytes || offset%MaximumVideoChunkBytes != 0 || (offset > 0 && revision == "") {
		return VideoChunk{}, VideoRange
	}
	// Resolve the authorized publication root, then keep path traversal rooted
	// during open. Do not open the earlier resolved absolute file name.
	dir, err := os.OpenRoot(ResolvedPath(root))
	if err != nil {
		return VideoChunk{}, NotFound
	}
	defer dir.Close()
	f, err := dir.Open(name)
	if err != nil {
		return VideoChunk{}, NotFound
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return VideoChunk{}, NotFound
	}
	if before.Size() > MaximumVideoBytes {
		return VideoChunk{}, TooLarge
	}
	version := strconv.FormatInt(before.Size(), 16) + "-" + strconv.FormatInt(before.ModTime().UnixNano(), 16)
	if revision != "" && revision != version {
		return VideoChunk{}, VideoChanged
	}
	header := make([]byte, 512)
	n, err := f.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return VideoChunk{}, NotFound
	}
	if http.DetectContentType(header[:n]) != "video/mp4" {
		return VideoChunk{}, VideoInvalid
	}
	if err := ctx.Err(); err != nil {
		return VideoChunk{}, err
	}
	count := min(int64(MaximumVideoChunkBytes), before.Size()-offset)
	if count <= 0 {
		return VideoChunk{}, VideoRange
	}
	data := make([]byte, int(count))
	if _, err := f.ReadAt(data, offset); err != nil {
		return VideoChunk{}, VideoChanged
	}
	after, err := f.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return VideoChunk{}, VideoChanged
	}
	if err := ctx.Err(); err != nil {
		return VideoChunk{}, err
	}
	return VideoChunk{MediaType: "video/mp4", TotalBytes: before.Size(), Offset: offset, Revision: version, ByteCount: len(data), Data: data}, nil
}

// VideoRefusal describes the media-specific errors without changing text errors.
func VideoRefusal(err error) (int, string, string) {
	switch err {
	case VideoChanged:
		return 409, string(VideoChanged), "The video changed. Open it again."
	case VideoInvalid:
		return 415, string(VideoInvalid), "This file is not a supported MP4 video."
	case VideoRange:
		return 416, string(VideoRange), "The video chunk is outside the allowed range."
	case TooLarge:
		return 413, "video_too_large", fmt.Sprintf("The video exceeds %d MiB.", MaximumVideoBytes/(1024*1024))
	default:
		return 404, "video_not_found", "No published video named that."
	}
}
