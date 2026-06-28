package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// render.go draws the console. Layout is computed in cells (lipgloss can't flex)
// and every screen shares the theme.go palette/glyphs, so the four design states
// stay one coherent system.

// ---- cell helpers ----

// fit truncates (with …) or space-pads s to exactly w visible cells, ANSI-aware.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// bg fits s to w and paints a background across the whole width.
func bg(s string, w int, hex string) string {
	return lipgloss.NewStyle().Background(color(hex)).Render(fit(s, w))
}

// fitBlock normalizes a multiline block to exactly w×h cells.
func fitBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, h)
	for i := 0; i < h; i++ {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

// rowOf joins blocks left-to-right with a gap of blank columns.
func rowOf(gap int, blocks ...string) string {
	if gap <= 0 {
		return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
	}
	parts := make([]string, 0, len(blocks)*2-1)
	for i, b := range blocks {
		if i > 0 {
			parts = append(parts, strings.Repeat(" ", gap))
		}
		parts = append(parts, b)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// shares splits total into n cell widths proportional to weights, after
// reserving gap*(n-1) for gaps. Any rounding remainder lands on the widest.
func shares(total, gap int, weights []float64) []int {
	n := len(weights)
	avail := total - gap*(n-1)
	if avail < n {
		avail = n
	}
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	out := make([]int, n)
	used, big := 0, 0
	for i, w := range weights {
		out[i] = int(float64(avail) * w / sum)
		if out[i] < 8 {
			out[i] = 8
		}
		used += out[i]
		if weights[i] > weights[big] {
			big = i
		}
	}
	out[big] += avail - used
	return out
}

// labeledFrame draws a rounded box with floating labels on the top border. inner
// must already be (w-2)×(h-2). left/right are pre-styled label strings.
func labeledFrame(w, h int, left, right, borderHex, inner string) string {
	bc := fg(borderHex)
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	fill := w - 8 - lw - rw
	if fill < 1 {
		fill = 1
	}
	top := bc.Render("╭─ ") + left + bc.Render(" "+strings.Repeat("─", fill)+" ") + right + bc.Render(" ─╮")
	var b strings.Builder
	b.WriteString(fit(top, w) + "\n")
	for _, line := range strings.Split(fitBlock(inner, w-2, h-2), "\n") {
		b.WriteString(bc.Render("│") + line + bc.Render("│") + "\n")
	}
	b.WriteString(bc.Render("╰" + strings.Repeat("─", w-2) + "╯"))
	return b.String()
}

// ---- top bar / footer ----

func (m consoleModel) topBar(sub string) string {
	g, a, blk := m.counts()
	left := fg(cFocus).Bold(true).Render(gBrand+" omni") + " " + dimSt().Render(jp["omni"])
	if sub != "" {
		left += " " + faintSt().Render("·") + " " + dimSt().Render(sub)
	}
	dot := faintSt().Render(" · ")
	blocked := faintSt().Render("○ none blocked")
	if blk > 0 {
		mark := gBlocked
		if m.blink {
			mark = " "
		}
		blocked = fg(cBlock).Bold(true).Render(mark + " " + fmt.Sprintf("%d blocked", blk))
	}
	right := faintSt().Render(fmt.Sprintf("%s ", "")) +
		fg(cIdle).Bold(true).Render(fmt.Sprintf("%d", g)) + faintSt().Render(" groups") + dot +
		fg(cWork).Bold(true).Render(fmt.Sprintf("%d", a)) + faintSt().Render(" agents") + dot +
		blocked + dot + dimSt().Render(time.Now().Format("15:04"))
	gapN := m.w - ansi.StringWidth(left) - ansi.StringWidth(right) - 2
	if gapN < 1 {
		gapN = 1
	}
	return bg(" "+left+strings.Repeat(" ", gapN)+right+" ", m.w, cBar)
}

type hint struct{ key, label, kc string }

func footer(w int, left, right []hint) string {
	render := func(hs []hint) string {
		var parts []string
		for _, x := range hs {
			kc := cTxt
			if x.kc != "" {
				kc = x.kc
			}
			parts = append(parts, fg(kc).Bold(true).Render(x.key)+" "+dimSt().Render(x.label))
		}
		return strings.Join(parts, "   ")
	}
	l, r := render(left), render(right)
	gapN := w - ansi.StringWidth(l) - ansi.StringWidth(r) - 2
	if gapN < 1 {
		gapN = 1
	}
	return bg(" "+l+strings.Repeat(" ", gapN)+r+" ", w, cBar)
}

// ---- View ----

func (m consoleModel) View() string {
	if m.w == 0 {
		return "loading…"
	}
	if m.picker {
		return m.viewPicker()
	}
	if len(m.rooms) == 0 {
		return m.viewEmpty()
	}
	if m.glance {
		return m.viewGlance()
	}
	return m.viewHero()
}

func (m consoleModel) bodyH() int { return m.h - 2 }

func (m consoleModel) viewHero() string {
	top := m.topBar("~/work")
	foot := footer(m.w, []hint{
		{"↹", "focus", ""}, {"⏎", "expand", ""}, {"z", "compress", ""},
		{"^n", "new project", ""}, {"^b", "broadcast", cCast},
	}, []hint{{"!", "jump to blocked", cBlock}, {"?", "help", ""}})

	bh := m.bodyH()
	weights := make([]float64, len(m.rooms))
	for i, r := range m.rooms {
		weights[i] = 1 + 0.45*float64(len(r.agents)-1)
	}
	ws := shares(m.w-2, 2, weights)
	frames := make([]string, len(m.rooms))
	for i, r := range m.rooms {
		frames[i] = m.heroGroup(r, ws[i], bh)
	}
	body := fitBlock(" "+rowOf(2, frames...), m.w, bh)
	return top + "\n" + body + "\n" + foot
}

func (m consoleModel) heroGroup(r room, w, h int) string {
	left := fg(cIdle).Render(r.name) + faintSt().Render(" · "+groupRole(r))
	right := m.groupRightLabel(r)
	bc := cGrp
	inner := m.heroTiles(r, w-2, h-2)
	return labeledFrame(w, h, left, right, bc, inner)
}

// heroTiles lays out a group's tiles: a lone tile fills the frame; otherwise the
// lead takes a wider left column and the agents stack on the right.
func (m consoleModel) heroTiles(r room, w, h int) string {
	li := r.leadIndex()
	if len(r.agents) == 1 {
		return m.tile(r.agents[0], w, h, true)
	}
	leadW := w * 52 / 100
	rightW := w - leadW - 1
	lead := m.tile(r.agents[li], leadW, h, true)

	var others []session
	for i, a := range r.agents {
		if i != li {
			others = append(others, a)
		}
	}
	n := len(others)
	gap := 1
	avail := h - gap*(n-1)
	col := make([]string, 0, n*2-1)
	for i, a := range others {
		th := avail / n
		if i == n-1 {
			th = avail - (avail/n)*(n-1)
		}
		if i > 0 {
			col = append(col, strings.Repeat(" ", rightW))
		}
		col = append(col, m.tile(a, rightW, th, false))
	}
	rightCol := lipgloss.JoinVertical(lipgloss.Left, col...)
	return rowOf(1, lead, fitBlock(rightCol, rightW, h))
}

// tile renders one agent: header band + a representative body peek. wide=true is
// the lead/primary tile (gets the role badge).
func (m consoleModel) tile(s session, w, h int, wide bool) string {
	focused := s.ID == m.focusID
	border := lipgloss.RoundedBorder()
	bc, headBg := cBd, cBar
	switch {
	case focused:
		border, bc, headBg = lipgloss.DoubleBorder(), cFocus, "#10222b"
	case s.Status == "blocked":
		bc, headBg = cBlock, "#2a1212"
	}
	iw := w - 2
	header := m.tileHeader(s, iw, focused, wide)
	body := m.tileBody(s, iw, h-3) // -2 border, -1 header
	content := bg(header, iw, headBg) + "\n" + fitBlock(body, iw, h-3)
	return lipgloss.NewStyle().Border(border).BorderForeground(color(bc)).Render(content)
}

func (m consoleModel) tileHeader(s session, w int, focused, wide bool) string {
	sc := statusColor(s.Status)
	var b strings.Builder
	if s.Status == "blocked" {
		mark := gBlocked
		if m.blink {
			mark = " "
		}
		b.WriteString(fg(cBlock).Render(mark) + " " + fg(cBlock).Bold(true).Render(name(s)))
	} else if focused {
		b.WriteString(fg(cFocus).Render(gFocus+" ") + fg(cFocus).Bold(true).Render(gBrand+" "+name(s)))
	} else {
		b.WriteString(fg(sc).Render(statusGlyph(s.Status)) + " " + fg(cTxt).Bold(true).Render(name(s)))
	}
	if wide {
		b.WriteString(" " + badge("LEAD", ifFocus(focused)))
	}
	if s.CurrentActivity != "" && s.Status != "blocked" {
		b.WriteString(dimSt().Render(" · " + s.CurrentActivity))
	}
	left := b.String()
	// right side: status word + age, or BLOCKED badge + age
	var right string
	if s.Status == "blocked" {
		right = blockBadge() + " " + fg(cBlock).Bold(true).Render(since(s.LastEventAt))
	} else {
		right = fg(sc).Render(statusGlyph(s.Status)+" "+s.Status) + " " + faintSt().Render(since(s.LastEventAt))
	}
	gapN := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gapN < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gapN) + right
}

func (m consoleModel) tileBody(s session, w, h int) string {
	pad := func(lines ...string) string {
		for i := range lines {
			lines[i] = " " + lines[i]
		}
		return strings.Join(lines, "\n")
	}
	caret := gCaret
	if m.blink {
		caret = " "
	}
	if s.Status == "blocked" {
		q := blockedQuestion(s)
		return inset(pad(
			faintSt().Render("NEEDS DECISION · "+jp["decision"]),
			fg(cTxt).Bold(true).Render(q),
			"",
			fg(cBlock).Render("⏵ awaiting your input ")+fg(cFocus).Render(caret),
		), w, h)
	}
	act := s.CurrentActivity
	if act == "" {
		act = s.Status
	}
	lines := []string{
		fg(sc(s)).Render(gRun) + " " + dimSt().Render(act),
		faintSt().Render("  " + gBlock + " " + name(s) + " session active"),
		dimSt().Render(shortName(s)+" ") + fg(sc(s)).Render(gShell) + " " + fg(cFocus).Render(caret),
		"",
		faintSt().Render(gLive + " live terminal · " + paneLabel(s)),
	}
	return inset(pad(lines...), w, h)
}

// inset paints the tile body background (the terminal area) across w×h.
func inset(content string, w, h int) string {
	lines := strings.Split(content, "\n")
	out := make([]string, h)
	for i := 0; i < h; i++ {
		s := ""
		if i < len(lines) {
			s = lines[i]
		}
		out[i] = bg(s, w, cInset)
	}
	return strings.Join(out, "\n")
}

// ---- small label helpers ----

func name(s session) string {
	if s.Role == "" {
		return "agent"
	}
	return s.Role
}
func shortName(s session) string {
	if s.Room != "" && s.Room != noRoom {
		return s.Room + "-" + name(s)
	}
	return name(s)
}
func sc(s session) string { return statusColor(s.Status) }

// badge is a small inline chip (single line) approximating the design's bordered
// LEAD tag with brackets.
func badge(text, hex string) string {
	return fg(cGrp).Render("⟦") + fg(hex).Render(text) + fg(cGrp).Render("⟧")
}
func ifFocus(f bool) string {
	if f {
		return cFocus
	}
	return cIdle
}
func blockBadge() string {
	return lipgloss.NewStyle().Background(color(cBlock)).Foreground(color("#1a0605")).Bold(true).
		Render(" BLOCKED " + jp["blocked"] + " ")
}

func groupRole(r room) string {
	if len(r.agents) <= 1 {
		return "lead"
	}
	return fmt.Sprintf("lead+%d", len(r.agents)-1)
}

func (m consoleModel) groupRightLabel(r room) string {
	if r.blocked() > 0 {
		return fg(cBlock).Render(fmt.Sprintf("%d needs you", r.blocked()))
	}
	if len(r.agents) > 1 {
		return fg(cCast).Render(gCast + " message bus")
	}
	return faintSt().Render("1 session")
}

// blockedQuestion pulls the agent's pending question from the room's hcom bus
// (its latest message), falling back to a generic prompt.
func blockedQuestion(s session) string {
	r := room{name: s.Room, agents: []session{s}}
	if msgs, err := loadChat(roomHcomDB(r)); err == nil {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].from == name(s) && msgs[i].text != "" {
				return msgs[i].text
			}
		}
	}
	return "awaiting your decision"
}

func paneLabel(s session) string {
	if s.TmuxPane != "" {
		return "pane " + s.TmuxPane
	}
	return "starting"
}
