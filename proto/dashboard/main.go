// PROTOTYPE — throwaway. Run: go run ./proto/dashboard
//
// Question (v2): when you open a room, how should you SEE and TALK TO the agents
// in it? Each agent has its own chat thread; you should be able to read any of
// them and type into any of them, from inside the dashboard. Agents are dynamic
// — start any agent into a room at runtime (^a), not a fixed list.
//
//   [tab] toggles the in-room layout (the taste call):
//     - FOCUS : one agent's chat full-screen + a tab strip of the others.
//     - TILED : every agent's chat side-by-side at once (tmux-split feel).
//
// Drive it: ↑↓ switch agent · type a message · enter send · ^a add agent ·
// tab toggle · esc back · q quit. Sending to a BLOCKED agent unblocks it — that's
// the core loop (answer the agent that's stuck on a question).
//
// Fake in-memory data; no DB, no tmux, no real Claude. The bit worth keeping is
// the model + Update; the rendering is throwaway. Open design Qs → NOTES.md.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
	dim   = "\x1b[2m"
	rev   = "\x1b[7m"
	red   = "\x1b[31m"
	green = "\x1b[32m"
	yel   = "\x1b[33m"
	cyan  = "\x1b[36m"
)

type chatMsg struct{ from, text string } // from: "you", the role, or "sys"
type agent struct {
	role, status, activity string
	chat                   []chatMsg
}
type room struct {
	name   string
	agents []agent
}

type model struct {
	rooms   []room
	inRoom  bool // false = rooms list, true = a room is open
	tiled   bool // in-room layout: false = FOCUS, true = TILED
	width   int
	roomCur int // 0..len(rooms): last index = "+ new room"
	agtCur  int // focused agent in the open room
	compose string
	prompt  string // "" = none; "room" or "agent" = modal name entry
	input   string
}

func seed() []room {
	return []room{
		{"feature-x", []agent{
			{"frontend", "working", "Edit", []chatMsg{{"sys", "spawned"}, {"frontend", "Scaffolding the login form."}}},
			{"backend", "blocked", "", []chatMsg{{"sys", "spawned"}, {"backend", "Token format — JWT or opaque? Need this to design the session table."}}},
		}},
		{"payments", []agent{
			{"api", "working", "Bash", []chatMsg{{"api", "Running migrations."}}},
			{"worker", "idle", "", []chatMsg{{"worker", "Queue drained, waiting."}}},
		}},
		{"docs-site", []agent{
			{"writer", "done", "", []chatMsg{{"writer", "Docs published."}}},
		}},
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) curRoom() *room {
	if m.roomCur < len(m.rooms) {
		return &m.rooms[m.roomCur]
	}
	return nil
}

func (m *model) send() {
	r := m.curRoom()
	if r == nil || len(r.agents) == 0 || strings.TrimSpace(m.compose) == "" {
		m.compose = ""
		return
	}
	a := &r.agents[m.agtCur]
	a.chat = append(a.chat, chatMsg{"you", m.compose})
	a.chat = append(a.chat, chatMsg{a.role, "(got it — continuing)"})
	if a.status == "blocked" { // answering a stuck agent unblocks it — the core loop
		a.status, a.activity = "working", "thinking"
	}
	m.compose = ""
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Modal name entry (new room / add agent) captures everything.
	if m.prompt != "" {
		switch k.Type {
		case tea.KeyEnter:
			if name := strings.TrimSpace(m.input); name != "" {
				if m.prompt == "room" {
					m.rooms = append(m.rooms, room{name: name})
					m.roomCur = len(m.rooms) - 1
				} else { // agent
					if r := m.curRoom(); r != nil {
						r.agents = append(r.agents, agent{role: name, status: "starting", chat: []chatMsg{{"sys", "spawned"}}})
						m.agtCur = len(r.agents) - 1
					}
				}
			}
			m.prompt, m.input = "", ""
		case tea.KeyEsc:
			m.prompt, m.input = "", ""
		case tea.KeyBackspace:
			if m.input != "" {
				m.input = m.input[:len(m.input)-1]
			}
		case tea.KeySpace:
			m.input += " "
		case tea.KeyRunes:
			m.input += string(k.Runes)
		}
		return m, nil
	}

	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if !m.inRoom {
		switch k.String() {
		case "q":
			return m, tea.Quit
		case "up", "k":
			if m.roomCur > 0 {
				m.roomCur--
			}
		case "down", "j":
			if m.roomCur < len(m.rooms) {
				m.roomCur++
			}
		case "tab":
			m.tiled = !m.tiled
		case "n":
			m.prompt = "room"
		case "enter", "l", "right":
			if m.roomCur == len(m.rooms) {
				m.prompt = "room"
			} else if len(m.rooms[m.roomCur].agents) > 0 {
				m.agtCur, m.inRoom = 0, true
			}
		}
		return m, nil
	}

	// In a room: arrows switch agent, letters compose, enter sends.
	r := m.curRoom()
	switch k.String() {
	case "esc":
		m.inRoom, m.compose = false, ""
	case "tab":
		m.tiled = !m.tiled
	case "ctrl+a":
		m.prompt = "agent"
	case "up", "left":
		if m.agtCur > 0 {
			m.agtCur--
		}
	case "down", "right":
		if r != nil && m.agtCur < len(r.agents)-1 {
			m.agtCur++
		}
	case "enter":
		m.send()
	case "backspace":
		if m.compose != "" {
			m.compose = m.compose[:len(m.compose)-1]
		}
	default:
		switch k.Type {
		case tea.KeySpace:
			m.compose += " "
		case tea.KeyRunes:
			m.compose += string(k.Runes)
		}
	}
	return m, nil
}

// ---- rendering ----

func dot(status string) string {
	switch status {
	case "blocked":
		return red + "●" + reset
	case "working":
		return green + "●" + reset
	case "starting":
		return yel + "●" + reset
	default:
		return dim + "○" + reset
	}
}

func trunc(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s + strings.Repeat(" ", w-len(r))
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

func chatLines(a agent, w, max int) []string {
	chat := a.chat
	if len(chat) > max {
		chat = chat[len(chat)-max:]
	}
	var out []string
	for _, c := range chat {
		// Plain text — truncation to a fixed column width can't survive inline
		// ANSI, so tiled columns stay uncolored. FOCUS view colors instead.
		out = append(out, trunc(c.from+": "+c.text, w))
	}
	for len(out) < max {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

func (m model) viewRoomsList() string {
	var b strings.Builder
	b.WriteString("  " + bold + "ROOMS" + reset + "\n\n")
	for i, r := range m.rooms {
		blocked := 0
		for _, a := range r.agents {
			if a.status == "blocked" {
				blocked++
			}
		}
		flag := ""
		if blocked > 0 {
			flag = fmt.Sprintf("  %s%d blocked%s", red, blocked, reset)
		}
		line := fmt.Sprintf(" %-14s %d agents%s", r.name, len(r.agents), flag)
		if i == m.roomCur {
			line = rev + trunc(stripw(line), 30) + reset + flag
		}
		b.WriteString("  " + line + "\n")
	}
	nr := " + new room"
	if m.roomCur == len(m.rooms) {
		nr = rev + nr + " " + reset
	} else {
		nr = dim + nr + reset
	}
	b.WriteString("  " + nr + "\n")
	b.WriteString("\n  " + dim + "↑↓ move · enter open · n new room · tab layout · q quit" + reset + "\n")
	return b.String()
}

// stripw returns the line without the trailing blocked-flag (which carries ANSI)
// so it can be safely reverse-highlighted; the flag is re-appended uncolored-safe.
func stripw(s string) string {
	if i := strings.Index(s, "  \x1b"); i >= 0 {
		return s[:i]
	}
	return s
}

func (m model) viewRoomFocus(r *room) string {
	var b strings.Builder
	// tab strip of agents
	b.WriteString("  ")
	for i, a := range r.agents {
		tab := " " + dot(a.status) + " " + a.role + " "
		if i == m.agtCur {
			tab = rev + " " + a.role + " " + reset
		}
		b.WriteString(tab + " ")
	}
	b.WriteString("\n\n")
	a := r.agents[m.agtCur]
	fmt.Fprintf(&b, "  %s%s%s %s· %s%s\n", bold, a.role, reset, dim, a.status, reset)
	b.WriteString("  " + dim + strings.Repeat("─", 50) + reset + "\n")
	for _, c := range a.chat {
		who := dim + c.from + ":" + reset
		if c.from == "you" {
			who = cyan + "you:" + reset
		} else if c.from == a.role {
			who = bold + c.from + ":" + reset
		}
		b.WriteString("  " + who + " " + c.text + "\n")
	}
	b.WriteString("\n  " + cyan + "→ " + r.agents[m.agtCur].role + ":" + reset + " " + m.compose + "▌\n")
	b.WriteString("\n  " + dim + "↑↓ switch agent · type · enter send · ^a add agent · tab tiled · esc back" + reset + "\n")
	return b.String()
}

func (m model) viewRoomTiled(r *room) string {
	w := m.width
	if w <= 0 {
		w = 90
	}
	n := len(r.agents)
	colW := (w-4)/n - 2
	if colW < 16 {
		colW = 16
	}
	if colW > 32 {
		colW = 32
	}
	cols := make([][]string, n)
	for i, a := range r.agents {
		head := dot(a.status) + " " + a.role
		if i == m.agtCur {
			head = rev + " " + a.role + " " + reset + " " + dot(a.status)
		}
		col := []string{head, dim + strings.Repeat("─", colW) + reset}
		col = append(col, chatLines(a, colW, 5)...)
		cols[i] = col
	}
	var b strings.Builder
	rows := 0
	for _, c := range cols {
		if len(c) > rows {
			rows = len(c)
		}
	}
	for row := 0; row < rows; row++ {
		b.WriteString("  ")
		for _, c := range cols {
			cell := strings.Repeat(" ", colW)
			if row < len(c) {
				cell = c[row]
			}
			b.WriteString(cell + dim + " │ " + reset)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n  " + cyan + "→ " + r.agents[m.agtCur].role + ":" + reset + " " + m.compose + "▌\n")
	b.WriteString("\n  " + dim + "←→ switch agent · type · enter send · ^a add agent · tab focus · esc back" + reset + "\n")
	return b.String()
}

func (m model) View() string {
	mode := "FOCUS"
	if m.tiled {
		mode = "TILED"
	}
	header := fmt.Sprintf("  %sOMNISCIENCE%s   %sin-room: %s%s\n\n", bold, reset, dim, mode, reset)

	if m.prompt != "" {
		label := "New room name:"
		if m.prompt == "agent" {
			label = "Start agent (role):"
		}
		return header + fmt.Sprintf("  %s%s%s %s▌\n\n  %senter create · esc cancel%s\n", bold, label, reset, m.input, dim, reset)
	}
	if !m.inRoom {
		return header + m.viewRoomsList()
	}
	r := m.curRoom()
	title := fmt.Sprintf("  %s%s%s%s   (esc: rooms)%s\n", bold, r.name, reset, dim, reset)
	if m.tiled {
		return header + title + "\n" + m.viewRoomTiled(r)
	}
	return header + title + "\n" + m.viewRoomFocus(r)
}

func main() {
	if _, err := tea.NewProgram(model{rooms: seed()}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "proto:", err)
		os.Exit(1)
	}
}
