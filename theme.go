package main

import "github.com/charmbracelet/lipgloss"

// theme.go is the design system from the Claude Design "Terminal Dashboard UI
// System" handoff, transcribed to lipgloss. Colors are the exact CSS-var hexes
// from the .dc.html screens; glyphs and labels match too. Keep this the single
// source of truth so every screen stays consistent.

// Palette — the --var hexes from the design.
const (
	cBg     = "#070a11" // outermost
	cScreen = "#0a0e16" // app background
	cPanel  = "#0c111b" // tile background
	cInset  = "#0e1420" // tile body / terminal area
	cBar    = "#0e131e" // top & bottom bars, tile headers
	cBd     = "#283246" // primary border
	cBd2    = "#1a2231" // faint border / dividers
	cGrp    = "#3c4d6b" // group frame
	cTxt    = "#c4cee0" // primary text
	cDim    = "#6b7891" // secondary text
	cFaint  = "#414c62" // tertiary / metadata
	cWork   = "#4fd6db" // status: working
	cBlock  = "#ff5347" // status: blocked
	cIdle   = "#94a3d4" // status: idle
	cDone   = "#4fd6a0" // status: done
	cFocus  = "#79e9ee" // focused tile / accents
	cCast   = "#f56fd0" // broadcast / message bus
)

// Glyphs — the exact runes used across the screens.
const (
	gBrand   = "◆" // omni / lead marker
	gBlocked = "◍" // blocked (pulses)
	gWorking = "●" // working
	gIdle    = "○" // idle
	gDone    = "✓" // done
	gRun     = "⏵" // prompt / running line
	gShell   = "❯" // shell prompt
	gLive    = "⠿" // live-terminal footnote
	gFocus   = "⌖" // focus reticle
	gCast    = "✷" // broadcast
	gCaret   = "▌" // blinking caret
	gRow     = "▸" // picker row marker
	gRecent  = "↺" // recent entry
	gSearch  = "⌕" // picker search
	gBlock   = "░" // faint block / empty slot
)

// jp holds the Japanese accent labels the design sprinkles in.
var jp = map[string]string{
	"omni":     "オムニ",
	"screens":  "画面",
	"working":  "実行中",
	"blocked":  "要対応",
	"idle":     "待機",
	"done":     "完了",
	"glance":   "俯瞰",
	"open":     "プロジェクトを開く",
	"recents":  "履歴",
	"launch":   "起動",
	"decision": "判断待ち",
}

func color(hex string) lipgloss.Color { return lipgloss.Color(hex) }

// fg/bg builders kept terse — most styles are one-offs built inline at the call
// site, but these cover the common cases.
func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(color(hex)) }
func dimSt() lipgloss.Style        { return fg(cDim) }
func faintSt() lipgloss.Style      { return fg(cFaint) }

// statusColor maps a session status to its design color.
func statusColor(status string) string {
	switch status {
	case "working", "starting":
		return cWork
	case "blocked":
		return cBlock
	case "idle":
		return cIdle
	case "done":
		return cDone
	default:
		return cDim
	}
}

// statusGlyph maps a session status to its design glyph.
func statusGlyph(status string) string {
	switch status {
	case "blocked":
		return gBlocked
	case "idle":
		return gIdle
	case "done":
		return gDone
	default:
		return gWorking
	}
}
