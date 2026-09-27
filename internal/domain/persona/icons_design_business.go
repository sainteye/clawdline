package persona

// The design and business teams' bots, registered one line each in icons.go.
// Each team is a family: design is pink to violet — colour swatches, a set
// square, a crown, a finish flag, a framed picture; business is deep blue
// with gold — a price tag, a heart, a headset, a rising chart, code
// brackets and a padlock.
const (
	designPink    = "#d8589a"
	designViolet  = "#8a5cd6"
	designMagenta = "#b04a8f"
	designPurple  = "#6f4bc4"
	designRose    = "#e07bb5"

	businessNavy     = "#1f3a6e"
	businessCobalt   = "#2c55a0"
	businessDenim    = "#2b4a7d"
	businessSteel    = "#1f5f96"
	businessIndigo   = "#3549a8"
	businessMidnight = "#18335e"

	businessGold = "#e8b64c"
)

var (
	uiDesignerBot = art{accent: 'B', palette: map[byte]string{'B': designPink, 'o': ink, 'A': "#ffd6e7", 'r': "#ff6b6b", 'y': "#ffd166", 'c': "#5fd4f4"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".BBBBBB.",
			".BrycBB.",
			"..B..B..",
		}}
	uxArchitectBot = art{accent: 'B', palette: map[byte]string{'B': designViolet, 'o': ink, 'S': "#f0e0ff"},
		rows: [iconHeight]string{
			"......S.",
			".....SS.",
			".BBBBSSS",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}}
	brandGuardianBot = art{accent: 'B', palette: map[byte]string{'B': designMagenta, 'o': ink, 'Y': "#f2c14e", 'r': "#e5484d"},
		rows: [iconHeight]string{
			".Y.YY.Y.",
			".YYrrYY.",
			".BBBBBB.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}}
	uiFinishGateBot = art{accent: 'B', palette: map[byte]string{'B': designPurple, 'o': ink, 'p': "#c9c9d1", 'k': ink, 'W': "#f4efe3"},
		rows: [iconHeight]string{
			"..pkWkW.",
			"..pWkWk.",
			".BpBBBB.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}}
	imagePromptBot = art{accent: 'B', palette: map[byte]string{'B': designRose, 'o': ink, 'A': "#ffd6e7", 'F': "#f2c14e", 'm': "#2f9e5b", 'S': "#9fd8ff", 'y': "#fff1c7"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".FFFFFF.",
			".FmmSyF.",
			".FFFFFF.",
		}}

	pricingBot = art{accent: 'B', palette: map[byte]string{'B': businessNavy, 'o': "#f4efe3", 'T': businessGold, 'k': ink},
		rows: [iconHeight]string{
			"....TTTT",
			"...TkTTT",
			".BBBTTTB",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}}
	customerSuccessBot = art{accent: 'B', palette: map[byte]string{'B': businessCobalt, 'o': ink, 'H': businessGold},
		rows: [iconHeight]string{
			".....H.H",
			".....HHH",
			".BBBBBH.",
			".BoBBoB.",
			".BBBBBB.",
			".BBBBBB.",
			"..B..B..",
		}}
	supportBot = art{accent: 'B', palette: map[byte]string{'B': businessDenim, 'o': "#f4efe3", 'h': businessGold},
		rows: [iconHeight]string{
			"..hhhh..",
			".h....h.",
			"hBBBBBBh",
			"hBoBBoBh",
			".BBBBBBh",
			".BBBBhh.",
			"..B..B..",
		}}
	analyticsBot = art{accent: 'B', palette: map[byte]string{'B': businessSteel, 'o': ink, 'A': "#cfe3ff", 'D': "#0f1f3d", 'Y': businessGold},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".DDDDYD.",
			".DDDYYD.",
			".DDYYYD.",
		}}
	devrelBot = art{accent: 'B', palette: map[byte]string{'B': businessIndigo, 'o': ink, 'A': "#cfe3ff", 'Y': businessGold, 'S': "#f4efe3"},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoBBoB.",
			".BYBSYB.",
			".YBSBBY.",
			"..Y..Y..",
		}}
	privacyBot = art{accent: 'B', palette: map[byte]string{'B': businessMidnight, 'o': "#f4efe3", 'A': "#cfe3ff", 'Y': businessGold, 'k': ink},
		rows: [iconHeight]string{
			"...AA...",
			"...BB...",
			".BBBBBB.",
			".BoYYoB.",
			".BYBBYB.",
			".BYYYYB.",
			".BYkYYB.",
		}}
)
