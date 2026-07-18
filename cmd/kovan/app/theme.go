package app

import "github.com/charmbracelet/lipgloss"

// The palette: color means "this needs Bora". One accent for needs-you, the
// gold brand chip, and a grayscale ramp for everything else. Every color
// literal lives here so the whole TUI retunes from one place.
var (
	colorAccent = lipgloss.Color("214") // needs-you: the one attention color
	colorBrand  = lipgloss.Color("220") // honey-gold: the kovan brand chip
	colorInk    = lipgloss.Color("234") // dark ink: foreground on the gold chip
	colorErr    = lipgloss.Color("203") // transient error messages only

	// The neutral ramp, dimmest to brightest. States, pins, and the cursor
	// pick a rung; "one step brighter" means the next rung up.
	colorFaint  = lipgloss.Color("238") // archived: set aside
	colorDim    = lipgloss.Color("242") // stopped rows and all chrome
	colorMid    = lipgloss.Color("246") // idle
	colorFg     = lipgloss.Color("252") // working, info messages
	colorBright = lipgloss.Color("231") // top rung: pinned working, active tab

	colorCursorBg = lipgloss.Color("237") // selected-row background
)

// grayRamp orders the neutral rungs dimmest to brightest for brighten.
var grayRamp = []lipgloss.Color{colorFaint, colorDim, colorMid, colorFg, colorBright}

// brighten lifts a ramp color one rung — the pinned and cursor-row emphasis.
// The top rung and non-ramp colors (the accent) stay put.
func brighten(c lipgloss.Color) lipgloss.Color {
	for i, r := range grayRamp[:len(grayRamp)-1] {
		if r == c {
			return grayRamp[i+1]
		}
	}
	return c
}

// stateColor maps an agent's state to the ramp: needs-you is the only state
// that gets color; the rest read as brightness, working down to archived.
func stateColor(state string) lipgloss.Color {
	switch state {
	case "needs-you":
		return colorAccent
	case "working":
		return colorFg
	case "idle":
		return colorMid
	case "archived":
		return colorFaint
	default: // stopped
		return colorDim
	}
}

// rowTints maps an agent's manifest color name to its stripe hue — the vivid
// foreground the board's left-edge glyph is drawn in. rowTintNames is the
// same palette in cycling order for the pickers.
var rowTints = map[string]lipgloss.Color{
	"red":     "160",
	"orange":  "208",
	"yellow":  "178",
	"green":   "71",
	"cyan":    "37",
	"blue":    "33",
	"magenta": "133",
	"grey":    "245",
}

// stripeGlyph marks a color-tagged row. Full cell height, so consecutive
// tagged rows read as one continuous bar.
const stripeGlyph = "▌"

// colorPreview shows a palette name with the stripe the row will carry, for
// the edit modal and new-agent form pickers ("none" has no stripe to show).
func colorPreview(name string) string {
	c, ok := rowTints[name]
	if !ok {
		return name
	}
	return lipgloss.NewStyle().Foreground(c).Render(stripeGlyph) + " " + name
}

var rowTintNames = []string{"red", "orange", "yellow", "green", "cyan", "blue", "magenta", "grey"}

var (
	brandStyle  = lipgloss.NewStyle().Bold(true).Background(colorBrand).Foreground(colorInk)
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colorDim)
	dimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	cursorStyle = lipgloss.NewStyle().Bold(true)
	accentStyle = lipgloss.NewStyle().Foreground(colorAccent)
	// tabOnStyle is the current board tab: bold + bright, so it stands out from
	// the dim inactive tab and the dim header context around it.
	tabOnStyle = lipgloss.NewStyle().Bold(true).Foreground(colorBright)
	// selStyle marks a picker's current choice when the field is not focused:
	// brighter than its dim siblings, but calmer than the bold focused row.
	selStyle = lipgloss.NewStyle().Foreground(colorFg)
	// helpKeyStyle lifts key names a step over their dim descriptions.
	helpKeyStyle = lipgloss.NewStyle().Foreground(colorMid)
	infoStyle    = lipgloss.NewStyle().Foreground(colorFg)
	errStyle     = lipgloss.NewStyle().Foreground(colorErr)
	boxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)
