// Command splashgen publishes the daemon's launch images as static console
// assets. Cloud hosting has no daemon to draw the PNGs at request time.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sainteye/clawdline/internal/domain/icon"
	"golang.org/x/net/html"
)

func main() {
	check := flag.Bool("check", false, "verify the committed launch images without writing")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if err := generate(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(root string, check bool) error {
	console := filepath.Join(root, "web", "console")
	body, err := os.ReadFile(filepath.Join(console, "index.html"))
	if err != nil {
		return err
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return err
	}
	count := 0
	var visit func(*html.Node) error
	visit = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "link" {
			attrs := map[string]string{}
			for _, attr := range n.Attr {
				attrs[attr.Key] = attr.Val
			}
			if attrs["rel"] == "apple-touch-startup-image" {
				var width, height int
				if _, err := fmt.Sscanf(attrs["href"], "/splash-%dx%d.png", &width, &height); err != nil {
					return fmt.Errorf("invalid launch image URL %q", attrs["href"])
				}
				name := "splash-" + strconv.Itoa(width) + "x" + strconv.Itoa(height) + ".png"
				if attrs["href"] != "/"+name {
					return fmt.Errorf("noncanonical launch image URL %q", attrs["href"])
				}
				body, ok := icon.Splash(width, height)
				if !ok {
					return fmt.Errorf("unsupported launch image %s", name)
				}
				path := filepath.Join(console, "public", name)
				if check {
					got, err := os.ReadFile(path)
					if err != nil || !samePixels(got, body) {
						return fmt.Errorf("%s is missing or stale; run go run ./internal/domain/icon/cmd/splashgen", name)
					}
				} else if err := os.WriteFile(path, body, 0644); err != nil {
					return err
				}
				count++
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(doc); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("no launch images declared in console index.html")
	}
	fmt.Printf("splashgen: %d launch images match the daemon renderer\n", count)
	return nil
}

// PNG compression bytes are not stable across Go versions. The published
// asset must decode to the same dimensions, colours and alpha as the renderer.
func samePixels(actual, expected []byte) bool {
	a, err := png.Decode(bytes.NewReader(actual))
	if err != nil {
		return false
	}
	b, err := png.Decode(bytes.NewReader(expected))
	if err != nil || a.Bounds() != b.Bounds() {
		return false
	}
	normalize := func(src image.Image) []byte {
		dst := image.NewNRGBA(src.Bounds())
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
		return dst.Pix
	}
	return bytes.Equal(normalize(a), normalize(b))
}
