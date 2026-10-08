package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
)

// itemImage reads an image named by the complete item response and saves its
// verified bytes to a new file. The filename is chosen by the caller so an
// agent can open the pixels with its image-reading tool.
func itemImage(stdout, stderr io.Writer, b *broker, itemID, imageID, output string) int {
	it, code := readItem(stdout, stderr, b, "item image", itemID)
	if code != 0 {
		return code
	}
	if it.Images == nil {
		fmt.Fprintln(stderr, cliCopy("item", "show.images_unavailable", "clawdline item show: the item response omitted its image inventory; image status is unknown."))
		return 1
	}
	var selected *itemImageWire
	for i := range *it.Images {
		if (*it.Images)[i].ID == imageID {
			selected = &(*it.Images)[i]
			break
		}
	}
	if selected == nil {
		fmt.Fprintf(stderr, cliCopy("item", "image.not_on_item", "clawdline item image: image %s is not on item %s.\n"), imageID, itemID)
		return 1
	}
	if selected.ByteCount <= 0 {
		fmt.Fprintf(stderr, cliCopy("item", "image.bad_metadata", "clawdline item image: image %s has invalid byte-count metadata.\n"), imageID)
		return 1
	}
	req, err := http.NewRequest(http.MethodGet, b.base+"/v1/work/v2/images/"+url.PathEscape(imageID), nil)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline item image: %v\n", err)
		return 1
	}
	req.Header.Set("X-Clawdline-Orchestrator", b.token)
	res, err := b.client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "clawdline item image: %v\n", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(res.Body, brokerAnswerLimit+1))
		if readErr != nil {
			fmt.Fprintf(stderr, "clawdline item image: %v\n", readErr)
			return 1
		}
		return report(stdout, stderr, "item image", answer{Status: res.StatusCode, Body: b.mask(body)})
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, selected.ByteCount+1))
	if err != nil || int64(len(data)) != selected.ByteCount || res.Header.Get("Content-Type") != selected.MediaType {
		fmt.Fprintf(stderr, cliCopy("item", "image.incomplete", "clawdline item image: image %s could not be read completely; no file was saved.\n"), imageID)
		return 1
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || "image/"+format != selected.MediaType || config.Width != selected.Width || config.Height != selected.Height {
		fmt.Fprintf(stderr, cliCopy("item", "image.invalid", "clawdline item image: image %s did not match its metadata; no file was saved.\n"), imageID)
		return 1
	}
	f, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("item", "image.output_error", "clawdline item image: cannot create %s: %v\n"), output, err)
		return 1
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(output)
		fmt.Fprintf(stderr, cliCopy("item", "image.write_error", "clawdline item image: could not save %s: %v\n"), output, firstError(writeErr, closeErr))
		return 1
	}
	fmt.Fprintf(stdout, cliCopy("item", "image.saved", "Saved full image %s to %s (%d bytes).\n"), imageID, output, len(data))
	return 0
}

func firstError(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
