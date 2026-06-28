package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// render2.go holds the empty, glance and picker screens + project scanning,
// keeping render.go focused on the hero/tiles.

// ---- empty ----

func (m consoleModel) viewEmpty() string {
	top := m.topBar("v0.4.2")
	foot := footer(m.w, []hint{{"^n", "new project", ""}, {"⏎", "resume", ""}, {"↹", "focus", ""}}, []hint{{"?", "help", ""}})
	bh := m.bodyH()

	title := lipgloss.JoinHorizontal(lipgloss.Bottom,
		fg(cTxt).Bold(true).Render("omni"), "  ", dimSt().Render(jp["omni"]))
	rows := []string{
		center(title, 50),
		"",
		center(dimSt().Render("run & watch many live coding sessions, side by side"), 50),
		"",
		fg(cFocus).Render("⏵ ") + chip("^n", cBd) + " " + fg(cTxt).Render("open a project"),
		dimSt().Render("⏎ ") + chip("⏎", cBd2) + " " + dimSt().Render("resume last — ") + fg(cIdle).Render("~/work/acme-web"),
		"",
		faintSt().Render("RECENTS · " + jp["recents"]),
	}
	for _, r := range recentProjects(3) {
		rows = append(rows, dimSt().Render(gRun+" ")+fg(cIdle).Render(r.path)+faintSt().Render("  "+r.recent))
	}
	card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(cBd2)).
		Padding(1, 3).Render(strings.Join(rows, "\n"))

	legend := center(legendStrip(), m.w)
	bodyInner := lipgloss.Place(m.w, bh-1, lipgloss.Center, lipgloss.Center, card)
	body := fitBlock(bodyInner, m.w, bh-1) + "\n" + bg(legend, m.w, cBar)
	return top + "\n" + fitBlock(body, m.w, bh) + "\n" + foot
}

func legendStrip() string {
	item := func(g, hex, label, jpk string) string {
		return fg(hex).Render(g) + " " + fg(cTxt).Render(label) + " " + faintSt().Render(jp[jpk])
	}
	return strings.Join([]string{
		faintSt().Render("LEGEND"),
		item(gWorking, cWork, "working", "working"),
		item(gBlocked, cBlock, "blocked", "blocked"),
		item(gIdle, cIdle, "idle", "idle"),
		item(gDone, cDone, "done", "done"),
		fg(cFocus).Render(gFocus) + " " + fg(cTxt).Render("focus"),
		fg(cCast).Render(gCast) + " " + fg(cTxt).Render("broadcast"),
	}, "   ")
}

// ---- glance ----

func (m consoleModel) viewGlance() string {
	top := m.topBar("glance mode · " + jp["glance"])
	jump := "all calm"
	jc := cDone
	if a := m.firstBlocked(); a != nil {
		jump = "jump to blocked → " + a.Room + "/" + name(*a)
		jc = cBlock
	}
	foot := footer(m.w, []hint{
		{"↹", "focus", ""}, {"⏎", "expand", ""}, {"z", "compress", ""}, {"^b", "broadcast", cCast},
	}, []hint{{"!", jump, jc}, {"?", "help", ""}})

	bh := m.bodyH()
	weights := make([]float64, len(m.rooms))
	for i, r := range m.rooms {
		weights[i] = 1 + 0.15*float64(len(r.agents)-1)
	}
	ws := shares(m.w-2, 2, weights)
	cols := make([]string, len(m.rooms))
	for i, r := range m.rooms {
		cols[i] = m.glanceGroup(r, ws[i], bh)
	}
	body := fitBlock(" "+rowOf(2, cols...), m.w, bh)
	return top + "\n" + body + "\n" + foot
}

func (m consoleModel) glanceGroup(r room, w, h int) string {
	left := fg(cIdle).Render(r.name) + faintSt().Render(" · "+groupCount(r))
	if r.blocked() > 0 {
		mark := gBlocked
		if m.blink {
			mark = " "
		}
		left = fg(cBlock).Bold(true).Render(mark+" "+r.name) + faintSt().Render(" · "+groupCount(r))
	}
	right := faintSt().Render("cross-repo")
	switch {
	case r.blocked() > 0:
		right = fg(cBlock).Render(fmt.Sprintf("%d needs you", r.blocked()))
	case len(r.agents) == 1:
		right = fg(cDone).Render("all calm")
	}

	li := r.leadIndex()
	var cards []string
	for i, a := range r.agents {
		if i > 0 {
			cards = append(cards, "") // 1-line gap
		}
		cards = append(cards, m.card(a, w-2, i == li))
	}
	inner := strings.Join(cards, "\n")
	used := len(r.agents)*cardH + (len(r.agents) - 1)
	if rem := (h - 2) - used; rem > 1 {
		filler := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(cBd2)).
			Width(w-4).Height(rem-3).Align(lipgloss.Center, lipgloss.Center).
			Render(faintSt().Render("group of one · 待機"))
		if len(r.agents) > 1 {
			filler = "" // only a lone group shows the placeholder
		}
		inner = inner + "\n" + fitBlock(filler, w-2, rem)
	}
	return labeledFrame(w, h, left, right, cGrp, inner)
}

const cardH = 6 // bordered card: border(2) + header(1) + 3 body lines

// card is a compressed "keep-an-eye" tile (glance mode): status at a glance plus
// two lines of context. A blocked card uses the red treatment and shouts.
func (m consoleModel) card(s session, w int, isLead bool) string {
	bc, headBg := cBd, ""
	if s.Status == "blocked" {
		bc, headBg = cBlock, "#2a1212"
		if m.blink {
			bc = "#7a211c" // edge pulse
		}
	}
	iw := w - 2
	header := m.tileHeader(s, iw, false, isLead)
	if headBg != "" {
		header = bg(header, iw, headBg)
	}
	var body []string
	if s.Status == "blocked" {
		caret := gCaret
		if m.blink {
			caret = " "
		}
		body = []string{
			" " + fg(cTxt).Bold(true).Render("needs decision: "+truncQ(blockedQuestion(s), iw-18)),
			" " + faintSt().Render(gRun+" last: "+activityOr(s)),
			" " + fg(cBlock).Render("⏎ expand to answer ") + fg(cFocus).Render(caret),
		}
	} else {
		body = []string{
			" " + dimSt().Render(summary(s)),
			" " + faintSt().Render(lastGlyph(s)+" last: "+activityOr(s)),
			"",
		}
	}
	content := header + "\n" + strings.Join(body, "\n")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(bc)).
		Width(iw).Render(content)
}

func summary(s session) string {
	switch s.Status {
	case "idle":
		return "waiting · ready for next task"
	case "done":
		return "complete"
	default:
		if s.CurrentActivity != "" {
			return s.CurrentActivity
		}
		return "working"
	}
}
func activityOr(s session) string {
	if s.CurrentActivity != "" {
		return s.CurrentActivity
	}
	return s.Status
}
func lastGlyph(s session) string {
	if s.Status == "done" {
		return fg(cDone).Render(gDone)
	}
	return dimSt().Render(gRun)
}
func truncQ(s string, w int) string {
	if w < 4 {
		w = 4
	}
	if ansi.StringWidth(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

// ---- picker ----

func (m consoleModel) viewPicker() string {
	// dimmed chrome behind (just the bars), modal centered over it
	top := m.topBar("~/work")
	bh := m.bodyH()
	modal := m.pickerModal(min(m.w-8, 72))
	body := lipgloss.Place(m.w, bh, lipgloss.Center, lipgloss.Top,
		lipgloss.NewStyle().MarginTop(3).Render(modal))
	foot := footer(m.w, []hint{{"↑↓", "move", ""}, {"⏎", "open", ""}, {"^g", "as new group", ""}}, []hint{{"esc", "cancel", ""}})
	return top + "\n" + fitBlock(body, m.w, bh) + "\n" + foot
}

func (m consoleModel) pickerModal(w int) string {
	iw := w - 2
	caret := gCaret
	if m.blink {
		caret = " "
	}
	hl := " " + fg(cFocus).Render(gSearch) + " " + fg(cTxt).Bold(true).Render("open project") + " " + faintSt().Render(jp["open"])
	hr := faintSt().Render("scan ~/work ~/src ~/dev") + " " + chip("esc", cBd)
	head := bg(spread(hl, hr, iw), iw, cBar)

	ql := " " + fg(cFocus).Render(gRun) + " " + fg(cTxt).Render(m.query) + fg(cFocus).Render(caret)
	qr := faintSt().Render(fmt.Sprintf("%d matches", len(m.results)))

	var rows []string
	rows = append(rows, fit(spread(ql, qr, iw), iw))
	rows = append(rows, fg(cBd2).Render(strings.Repeat("─", iw)))
	shown := m.results
	if len(shown) > 8 {
		shown = shown[:8]
	}
	for i, p := range shown {
		rows = append(rows, m.pickerRow(p, i == m.pCur, iw))
	}
	if len(shown) == 0 {
		rows = append(rows, " "+faintSt().Render("no matches under ~/work ~/src ~/dev"))
	}

	box := head + "\n" + strings.Join(rows, "\n")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(cFocus)).
		Background(color(cPanel)).Width(iw).Render(box)
}

func (m consoleModel) pickerRow(p proj, sel bool, w int) string {
	dir, base := splitPath(p.path)
	mark := faintSt().Render(gRow)
	label := dimSt().Render(dir) + fg(cTxt).Render(base)
	bglyph, bcol := gIdle, cFaint
	if sel {
		mark = fg(cFocus).Bold(true).Render(gRow)
		label = dimSt().Render(dir) + fg(cTxt).Bold(true).Render(base)
		bglyph, bcol = gWorking, cWork
	}
	left := " " + mark + " " + label
	right := fg(bcol).Render(bglyph+" "+p.branch) + faintSt().Render("  "+p.recent)
	row := spread(left, right+" ", w)
	if sel {
		return bg(row, w, cInset)
	}
	return fit(row, w)
}

// spread lays left and right on a w-wide line with the gap between them.
func spread(left, right string, w int) string {
	gapN := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gapN < 1 {
		gapN = 1
	}
	return left + strings.Repeat(" ", gapN) + right
}

// ---- project scan ----

func projectRoots() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, sub := range []string{"work", "src", "dev"} {
		out = append(out, filepath.Join(home, sub))
	}
	return out
}

// scanProjects lists immediate child dirs of the project roots, fuzzy-filtered by
// query (subsequence match), recents first-ish, capped.
func scanProjects(query string) []proj {
	home, _ := os.UserHomeDir()
	var out []proj
	for _, root := range projectRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			abs := filepath.Join(root, e.Name())
			disp := strings.Replace(abs, home, "~", 1)
			if !subseq(strings.ToLower(query), strings.ToLower(disp)) {
				continue
			}
			out = append(out, proj{path: disp, abs: abs, branch: gitBranch(abs)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func recentProjects(n int) []proj {
	all := scanProjects("")
	if len(all) > n {
		all = all[:n]
	}
	for i := range all {
		all[i].recent = []string{"2h ago", "yesterday", "3d ago"}[i%3]
	}
	return all
}

func gitBranch(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		return "—"
	}
	s := strings.TrimSpace(string(b))
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return "main"
}

func (m consoleModel) firstBlocked() *session {
	for gi := range m.rooms {
		for ti := range m.rooms[gi].agents {
			if m.rooms[gi].agents[ti].Status == "blocked" {
				return &m.rooms[gi].agents[ti]
			}
		}
	}
	return nil
}

// ---- tiny helpers ----

func center(s string, w int) string { return lipgloss.PlaceHorizontal(w, lipgloss.Center, s) }
func chip(text, hex string) string {
	return fg(hex).Render("[") + fg(cTxt).Render(text) + fg(hex).Render("]")
}
func groupCount(r room) string {
	if len(r.agents) == 1 {
		return "1"
	}
	return groupRole(r)
}
func splitPath(p string) (dir, base string) {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i+1], p[i+1:]
	}
	return "", p
}
func subseq(q, s string) bool {
	if q == "" {
		return true
	}
	i := 0
	for _, c := range s {
		if i < len(q) && rune(q[i]) == c {
			i++
		}
	}
	return i == len(q)
}
func spaceFill(total, used int) string {
	if total-used < 1 {
		return " "
	}
	return strings.Repeat(" ", total-used)
}
func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
