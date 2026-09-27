package persona

import (
	"fmt"
	"regexp"
)

// Icon is a persona's badge in the shape a project's mark has on the wire
// (common.schema.json Icon): rows of `#RRGGBB`, nil for transparent, and the
// accent the console tints around it. The accent is the bot's body colour.
type Icon struct {
	Accent string
	Cells  [][]*string
}

// Every bot is the same eight-by-seven body — antenna, square head, two eyes —
// told apart by its colour and one accessory: a hard hat, server stripes, a
// screen face, a needle, glasses, a ticked chest, a shield, a pen; and for the
// marketing team a magnifier, a quill, a sparkle, a speech bubble, a camera,
// an envelope, an up-arrow and a megaphone.
const (
	iconWidth  = 8
	iconHeight = 7
)

// art is one bot as drawn: a palette character per cell, '.' transparent.
type art struct {
	accent  byte
	palette map[byte]string
	rows    [iconHeight]string
}

var hexColour = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func (a art) grid() (Icon, error) {
	accent, ok := a.palette[a.accent]
	if !ok {
		return Icon{}, fmt.Errorf("the accent %q is not in the palette", a.accent)
	}
	for ch, c := range a.palette {
		if !hexColour.MatchString(c) {
			return Icon{}, fmt.Errorf("palette %q is %q, not #rrggbb", ch, c)
		}
	}
	cells := make([][]*string, iconHeight)
	for y, row := range a.rows {
		if len(row) != iconWidth {
			return Icon{}, fmt.Errorf("row %d is %d wide, not %d", y, len(row), iconWidth)
		}
		cells[y] = make([]*string, iconWidth)
		for x := 0; x < iconWidth; x++ {
			if row[x] == '.' {
				continue
			}
			c, ok := a.palette[row[x]]
			if !ok {
				return Icon{}, fmt.Errorf("row %d column %d is %q, which the palette does not name", y, x, row[x])
			}
			cells[y][x] = &c
		}
	}
	return Icon{Accent: accent, Cells: cells}, nil
}

const ink = "#141416"

var icons = map[string]art{
	"architect": {accent: 'B', palette: map[byte]string{'B': "#6c7bd9", 'o': ink, 'Y': "#f2c14e", 'y': "#c9962a"},
		rows: [iconHeight]string{
			"..YYYY..",
			".YYYYYY.",
			"yyyyyyyy",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}},
	"backend": {accent: 'B', palette: map[byte]string{'B': "#3f8f86", 'o': ink, 'A': "#9be7c4", 'g': "#56e39a", 'd': "#24524c"},
		rows: [iconHeight]string{
			"...AA...",
			"...dd...",
			".BBBBBB.",
			".BoBBoB.",
			".dddddd.",
			".dgdgdg.",
			"..B..B..",
		}},
	"frontend": {accent: 'F', palette: map[byte]string{'F': "#2b3440", 'S': "#5fd4f4", 'o': ink, 'A': "#ff8fb1"},
		rows: [iconHeight]string{
			"...AA...",
			"...FF...",
			"FFFFFFFF",
			"FSSSSSSF",
			"FSoSSoSF",
			"FSSSSSSF",
			"FFFFFFFF",
		}},
	"minimal-change": {accent: 'B', palette: map[byte]string{'B': "#8b95a7", 'o': ink, 's': "#e8ecf2", 'r': "#e5484d"},
		rows: [iconHeight]string{
			"rr..s...",
			"..rrs...",
			".BBBsBB.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}},
	"code-reviewer": {accent: 'B', palette: map[byte]string{'B': "#a371f7", 'o': ink, 'G': "#f0f0f0", 'A': "#f0f0f0"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			"GGGGGGGG",
			".GoBBoG.",
			".BBBBBB.",
			"..B..B..",
		}},
	"reality-checker": {accent: 'B', palette: map[byte]string{'B': "#e0664f", 'o': ink, 'c': "#2f9e5b", 'W': "#f4efe3", 'A': "#ffd166"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".BWWWcB.",
			".BcWcWB.",
			".BWcWWB.",
		}},
	"security": {accent: 'B', palette: map[byte]string{'B': "#2ea56b", 'o': ink, 'A': "#d8f5e5", 'L': "#1d6e47"},
		rows: [iconHeight]string{
			"...AA...",
			".LLLLLL.",
			".BBBBBB.",
			".BoBBoB.",
			".BBBBBB.",
			"..BBBB..",
			"...BB...",
		}},
	"technical-writer": {accent: 'B', palette: map[byte]string{'B': "#3aa6b9", 'o': ink, 'p': "#f2c14e", 'n': ink, 'W': "#f4efe3"},
		rows: [iconHeight]string{
			"......pn",
			".....p..",
			".BBBBpB.",
			".BoBBoB.",
			".BBBBBB.",
			".BWWWWB.",
			"..B..B..",
		}},
	"seo": {accent: 'B', palette: map[byte]string{'B': "#d9822b", 'o': ink, 'G': "#9fd8ff", 'h': "#5a3a1a"},
		rows: [iconHeight]string{
			"....GGG.",
			"...G..G.",
			".BBBGGGB",
			".BoBBhoB",
			".BBBhBB.",
			".BBhBBB.",
			"..B..B..",
		}},
	"content-writer": {accent: 'B', palette: map[byte]string{'B': "#c2566b", 'o': ink, 'Q': "#f4efe3", 'n': ink, 'W': "#f4efe3", 'l': "#8c8c99"},
		rows: [iconHeight]string{
			"......QQ",
			".....Qn.",
			".BBBBnB.",
			".BoBBoB.",
			".BBBBBB.",
			".BWlWlB.",
			"..B..B..",
		}},
	"ai-search": {accent: 'B', palette: map[byte]string{'B': "#4f5bd5", 'o': "#c8f7ff", 'S': "#ffe066"},
		rows: [iconHeight]string{
			"S.....S.",
			".S...SSS",
			".BBBBBS.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}},
	"social-media": {accent: 'B', palette: map[byte]string{'B': "#1f9bd1", 'o': ink, 'W': "#f4efe3", 'd': ink},
		rows: [iconHeight]string{
			"..WWWWW.",
			"..WdWdW.",
			".BBBWWB.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}},
	"instagram": {accent: 'B', palette: map[byte]string{'B': "#d6307a", 'o': ink, 'C': "#2b2b33", 'L': "#ffb347", 'A': "#ffd6e7"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".CCCCCC.",
			".CCLLCC.",
			"..B..B..",
		}},
	"email": {accent: 'B', palette: map[byte]string{'B': "#e0a526", 'o': ink, 'W': "#f4efe3", 'v': "#b0503b", 'A': "#fff1c7"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".WWWWWW.",
			".WvWWvW.",
			".WWvvWW.",
		}},
	"growth": {accent: 'B', palette: map[byte]string{'B': "#5aa832", 'o': ink, 'U': "#e5484d"},
		rows: [iconHeight]string{
			"......U.",
			".....UUU",
			".BBBBBU.",
			".BoBBoU.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}},
	"pr": {accent: 'B', palette: map[byte]string{'B': "#8a5cc2", 'o': ink, 'M': "#e8ecf2", 'm': "#9aa3b2", 'A': "#ffd166"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoBM",
			".BBBBBMM",
			".BBBBmMM",
			"..B..B.M",
		}},
}
