package gui

import (
	"embed"
	"image/color"

	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
)

// The launcher sits next to a game that is mostly played at night, in caves.
// Its palette is that: near-black stone with a faint blue cast, and one warm
// light source — the torch — reserved for the single thing a player came to
// do, which is press Play. Selection and links take the launcher's own sky
// blue, the colour of the slab on the icon. Nothing else is coloured unless
// the colour says something: which loader an instance runs, whether a thing
// is on, off, good or broken.
type Palette struct {
	Bg      color.NRGBA // the window ground
	Surface color.NRGBA // rail, bars, cards
	Raised  color.NRGBA // rows, inputs
	Hover   color.NRGBA // a row under the pointer
	Line    color.NRGBA // borders
	LineDim color.NRGBA // dividers inside a surface

	Text    color.NRGBA
	TextMid color.NRGBA
	TextDim color.NRGBA

	Torch    color.NRGBA // the one action colour
	TorchInk color.NRGBA // text on torch
	Sky      color.NRGBA // selection, focus, links
	Good     color.NRGBA
	Bad      color.NRGBA
}

// Theme bundles Gio's material theme with the palette and the type roles.
type Theme struct {
	*material.Theme
	P Palette
}

//go:embed fonts/*.ttf
var fontFiles embed.FS

// Type roles. The display face is Chakra Petch: squared terminals with a
// hint of the block world, used sparingly for names and the Play button so
// it never tips into a gaming-peripheral look. IBM Plex Sans carries all
// running text, and its mono sibling everything that is really data —
// versions, sizes, paths, codes — so a number reads as a number.
const (
	faceDisplay = "Chakra Petch"
	faceBody    = "IBM Plex Sans"
	faceMono    = "IBM Plex Mono"
)

// loadFonts parses the embedded faces. It runs once at startup and costs a
// few milliseconds; a failure to parse a bundled file is a build defect, so
// it panics rather than silently falling back.
func loadFonts() []font.FontFace {
	load := func(name, family string, weight font.Weight) font.FontFace {
		data, err := fontFiles.ReadFile("fonts/" + name)
		if err != nil {
			panic("gui: missing embedded font " + name)
		}
		face, err := opentype.Parse(data)
		if err != nil {
			panic("gui: parsing " + name + ": " + err.Error())
		}
		return font.FontFace{Font: font.Font{Typeface: font.Typeface(family), Weight: weight}, Face: face}
	}
	return []font.FontFace{
		load("IBMPlexSans-Regular.ttf", faceBody, font.Normal),
		load("IBMPlexSans-Medium.ttf", faceBody, font.Medium),
		load("IBMPlexSans-SemiBold.ttf", faceBody, font.SemiBold),
		load("IBMPlexMono-Regular.ttf", faceMono, font.Normal),
		load("IBMPlexMono-Medium.ttf", faceMono, font.Medium),
		load("ChakraPetch-SemiBold.ttf", faceDisplay, font.SemiBold),
		load("ChakraPetch-Bold.ttf", faceDisplay, font.Bold),
	}
}

// NewTheme builds the application theme.
func NewTheme() *Theme {
	p := Palette{
		Bg:      rgb(0x0C0E12),
		Surface: rgb(0x13161B),
		Raised:  rgb(0x1A1E25),
		Hover:   rgb(0x222731),
		Line:    rgb(0x2B313C),
		LineDim: rgb(0x1E232B),

		Text:    rgb(0xECEEF1),
		TextMid: rgb(0xA7AEBB),
		TextDim: rgb(0x6F7785),

		Torch:    rgb(0xF5A524),
		TorchInk: rgb(0x1A1206),
		Sky:      rgb(0x5B8DEF),
		Good:     rgb(0x55C57A),
		Bad:      rgb(0xE5645A),
	}

	base := material.NewTheme()
	base.Shaper = text.NewShaper(text.WithCollection(loadFonts()))
	base.Palette.Bg = p.Bg
	base.Palette.Fg = p.Text
	base.Palette.ContrastBg = p.Torch
	base.Palette.ContrastFg = p.TorchInk
	base.TextSize = unit.Sp(14)

	return &Theme{Theme: base, P: p}
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

// alpha returns c at the given opacity.
func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// mix blends two colours; t=0 gives a, t=1 gives b.
func mix(a, b color.NRGBA, t float32) color.NRGBA {
	lerp := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return color.NRGBA{R: lerp(a.R, b.R), G: lerp(a.G, b.G), B: lerp(a.B, b.B), A: lerp(a.A, b.A)}
}

// loaderColor is the one place a loader's identity becomes a colour. They
// are the shades the projects use for themselves, nudged towards each other
// so they sit together on the rail.
func (t *Theme) loaderColor(l instance.LoaderType) color.NRGBA {
	switch l {
	case instance.LoaderNeoForge:
		return rgb(0xE8792F)
	case instance.LoaderForge:
		return rgb(0x8FA4C4)
	case instance.LoaderFabric:
		return rgb(0xDFC07A)
	case instance.LoaderQuilt:
		return rgb(0xA47BE0)
	default:
		return rgb(0x6FBF73)
	}
}

// Spacing. Four steps are enough; anything finer is an accident.
var (
	sp1 = unit.Dp(4)
	sp2 = unit.Dp(8)
	sp3 = unit.Dp(14)
	sp4 = unit.Dp(22)
)

// Type scale, in sp.
const (
	sizeDisplay = unit.Sp(26)
	sizeTitle   = unit.Sp(15)
	sizeBody    = unit.Sp(14)
	sizeSmall   = unit.Sp(12.5)
	sizeMono    = unit.Sp(12.5)
)

// --- type roles ---

// display sets a name in the display face: the instance on the workbench,
// the title of a dialog.
func (t *Theme) display(gtx layout.Context, txt string) layout.Dimensions {
	l := material.Label(t.Theme, sizeDisplay, txt)
	l.Font.Typeface = faceDisplay
	l.Font.Weight = font.SemiBold
	l.Color = t.P.Text
	l.MaxLines = 1
	return l.Layout(gtx)
}

// brand is the display face at body size, for the wordmark and Play.
func (t *Theme) brand(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	l := material.Label(t.Theme, sizeTitle, txt)
	l.Font.Typeface = faceDisplay
	l.Font.Weight = font.SemiBold
	l.Color = c
	l.MaxLines = 1
	return l.Layout(gtx)
}

// title heads a section.
func (t *Theme) title(gtx layout.Context, txt string) layout.Dimensions {
	l := material.Label(t.Theme, sizeTitle, txt)
	l.Font.Typeface = faceBody
	l.Font.Weight = font.SemiBold
	l.Color = t.P.Text
	l.MaxLines = 1
	return l.Layout(gtx)
}

// body is running text.
func (t *Theme) body(gtx layout.Context, txt string) layout.Dimensions {
	return t.text(gtx, txt, sizeBody, font.Normal, t.P.Text)
}

// bodyMedium is running text with a little weight, for names in lists.
func (t *Theme) bodyMedium(gtx layout.Context, txt string) layout.Dimensions {
	return t.text(gtx, txt, sizeBody, font.Medium, t.P.Text)
}

// mid is secondary running text.
func (t *Theme) mid(gtx layout.Context, txt string) layout.Dimensions {
	return t.text(gtx, txt, sizeBody, font.Normal, t.P.TextMid)
}

// small is a caption.
func (t *Theme) small(gtx layout.Context, txt string) layout.Dimensions {
	return t.text(gtx, txt, sizeSmall, font.Normal, t.P.TextDim)
}

// smallIn is a caption in a chosen colour.
func (t *Theme) smallIn(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	return t.text(gtx, txt, sizeSmall, font.Medium, c)
}

// mono sets data: versions, sizes, paths.
func (t *Theme) mono(gtx layout.Context, txt string) layout.Dimensions {
	return t.monoIn(gtx, txt, t.P.TextMid)
}

// monoIn is mono in a chosen colour.
func (t *Theme) monoIn(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	l := material.Label(t.Theme, sizeMono, txt)
	l.Font.Typeface = faceMono
	l.Color = c
	l.MaxLines = 1
	return l.Layout(gtx)
}

// wrapped is running text that may take several lines.
func (t *Theme) wrapped(gtx layout.Context, txt string, c color.NRGBA) layout.Dimensions {
	l := material.Label(t.Theme, sizeSmall, txt)
	l.Font.Typeface = faceBody
	l.Color = c
	return l.Layout(gtx)
}

func (t *Theme) text(gtx layout.Context, txt string, size unit.Sp, weight font.Weight, c color.NRGBA) layout.Dimensions {
	l := material.Label(t.Theme, size, txt)
	l.Font.Typeface = faceBody
	l.Font.Weight = weight
	l.Color = c
	l.MaxLines = 1
	return l.Layout(gtx)
}
