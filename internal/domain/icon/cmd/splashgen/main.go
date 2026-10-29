// Command splashgen publishes the daemon's launch images as static console
// assets. Cloud hosting has no daemon to draw the PNGs at request time.
package main

import (
	"bytes"
	"encoding/binary"
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
	brand := flag.Bool("brand", false, "also publish application and browser icons")
	check := flag.Bool("check", false, "verify the committed launch images without writing")
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	if *brand {
		if err := generateBrand(*root, *check); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
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

// generateBrand keeps every installed application tile on the same renderer.
func generateBrand(root string, check bool) error {
	write := func(name string, want []byte, kind string) error {
		path := filepath.Join(root, name)
		if !check {
			return os.WriteFile(path, want, 0644)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		same := false
		switch kind {
		case "png":
			same = samePixels(got, want)
		case "ico":
			same = len(got) > 22 && len(want) > 22 && bytes.Equal(got[:14], want[:14]) && binary.LittleEndian.Uint32(got[18:22]) == 22 && int(binary.LittleEndian.Uint32(got[14:18])) == len(got)-22 && samePixels(got[22:], want[22:])
		case "icns":
			a, b := iconChunks(got), iconChunks(want)
			same = a != nil && len(a) == len(b)
			for k, v := range b {
				if !samePixels(a[k], v) {
					same = false
				}
			}
		}
		if !same {
			return fmt.Errorf("%s is stale; run splashgen -brand", name)
		}
		return nil
	}
	for _, size := range []int{192, 512} {
		body, ok := icon.AppIcon(size, true)
		if !ok {
			return fmt.Errorf("cannot render %d icon", size)
		}
		if err := write(fmt.Sprintf("web/console/public/icon-%d.png", size), body, "png"); err != nil {
			return err
		}
	}
	small, _ := icon.AppIcon(64, false)
	header := make([]byte, 22)
	binary.LittleEndian.PutUint16(header[2:], 1)
	binary.LittleEndian.PutUint16(header[4:], 1)
	header[6] = 64
	header[7] = 64
	binary.LittleEndian.PutUint16(header[10:], 1)
	binary.LittleEndian.PutUint16(header[12:], 32)
	binary.LittleEndian.PutUint32(header[14:], uint32(len(small)))
	binary.LittleEndian.PutUint32(header[18:], 22)
	if err := write("web/console/public/favicon.ico", append(header, small...), "ico"); err != nil {
		return err
	}
	var chunks []byte
	for _, entry := range []struct {
		tag  string
		size int
		grid bool
	}{
		{"icp4", 16, false}, {"icp5", 32, false}, {"icp6", 64, false}, {"ic07", 128, true}, {"ic08", 256, true}, {"ic09", 512, true}, {"ic10", 1024, true},
	} {
		body, _ := icon.AppIcon(entry.size, entry.grid)
		head := make([]byte, 8)
		copy(head, entry.tag)
		binary.BigEndian.PutUint32(head[4:], uint32(len(body)+8))
		chunks = append(chunks, head...)
		chunks = append(chunks, body...)
	}
	head := make([]byte, 8)
	copy(head, "icns")
	binary.BigEndian.PutUint32(head[4:], uint32(len(chunks)+8))
	if err := write("shell/darwin/Resources/Clawdline.icns", append(head, chunks...), "icns"); err != nil {
		return err
	}
	fmt.Println("splashgen: application, home-screen and browser icons match the connected mark")
	return nil
}

func iconChunks(body []byte) map[string][]byte {
	if len(body) < 8 || string(body[:4]) != "icns" || int(binary.BigEndian.Uint32(body[4:8])) != len(body) {
		return nil
	}
	out := map[string][]byte{}
	for p := 8; p < len(body); {
		if p+8 > len(body) {
			return nil
		}
		n := int(binary.BigEndian.Uint32(body[p+4 : p+8]))
		if n < 8 || p+n > len(body) {
			return nil
		}
		key := string(body[p : p+4])
		if _, ok := out[key]; ok {
			return nil
		}
		out[key] = body[p+8 : p+n]
		p += n
	}
	return out
}
