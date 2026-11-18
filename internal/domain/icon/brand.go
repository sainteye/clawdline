package icon

import (
	_ "embed"
	"encoding/xml"
	"image/color"
	"io"
	"strconv"
	"strings"
)

// BrandSVG is the selected rounded-square artwork. splashgen publishes the same
// source to the console so browser and installed marks cannot drift apart.
//
//go:embed brand.svg
var BrandSVG string

type brandRect struct {
	x, y, width, height, radius float64
	paint                       color.NRGBA
}

type brandStop struct{ offset, shade float64 }

type brandArtwork struct {
	x, y, width, height              float64
	lightX, lightY, lightDX, lightDY float64
	rects                            []brandRect
	stops                            []brandStop
}

var appArtwork = readBrandArtwork()

// The bundled source uses flat rounded rectangles with one directional light.
// This reads that artwork, rather than maintaining a second pixel-grid design.
func readBrandArtwork() brandArtwork {
	var art brandArtwork
	d := xml.NewDecoder(strings.NewReader(BrandSVG))
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		e, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, a := range e.Attr {
			attrs[a.Name.Local] = a.Value
		}
		number := func(key string) float64 { n, _ := strconv.ParseFloat(attrs[key], 64); return n }
		switch e.Name.Local {
		case "svg":
			values := strings.Fields(attrs["viewBox"])
			if len(values) != 4 {
				panic("brand artwork needs a viewBox")
			}
			art.x, _ = strconv.ParseFloat(values[0], 64)
			art.y, _ = strconv.ParseFloat(values[1], 64)
			art.width, _ = strconv.ParseFloat(values[2], 64)
			art.height, _ = strconv.ParseFloat(values[3], 64)
		case "linearGradient":
			art.lightX, art.lightY = number("x1"), number("y1")
			art.lightDX, art.lightDY = number("x2")-art.lightX, number("y2")-art.lightY
		case "stop":
			shade := number("stop-opacity")
			if attrs["stop-color"] == "black" {
				shade = -shade
			}
			art.stops = append(art.stops, brandStop{number("offset"), shade})
		case "rect":
			fill := attrs["fill"]
			if len(fill) != 7 || fill[0] != '#' {
				continue
			}
			rgb, err := strconv.ParseUint(fill[1:], 16, 32)
			if err != nil {
				panic(err)
			}
			art.rects = append(art.rects, brandRect{number("x"), number("y"), number("width"), number("height"), number("rx"), color.NRGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 255}})
		}
	}
	if art.width <= 0 || art.height <= 0 || len(art.rects) == 0 {
		panic("empty brand artwork")
	}
	return art
}

func (art brandArtwork) shade(t float64) float64 {
	previous := art.stops[0]
	if t <= previous.offset {
		return previous.shade
	}
	for _, next := range art.stops[1:] {
		if t <= next.offset {
			return previous.shade + (next.shade-previous.shade)*(t-previous.offset)/(next.offset-previous.offset)
		}
		previous = next
	}
	return previous.shade
}
