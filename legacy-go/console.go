package main

import (
	"database/sql"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// console.go is the omni dashboard — the Claude Design "Terminal Dashboard UI
// System" rendered live. It draws the whole compositor itself (top bar, group
// frames, agent tiles, footer, picker) over a 500ms poll of state.db. The tile
// bodies are a representative peek of each agent (status + activity); the real
// interactive Claude is reached with ⏎ expand (tmux attach). Closing the
// dashboard never touches the running agents.

func runTUI() error {
	db, err := openDB(dbPath())
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(newConsole(db), tea.WithAltScreen()).Run()
	db.Close()
	return err
}

type consoleModel struct {
	db    *sql.DB
	w, h  int
	rooms []room
	err   error

	glance  bool   // false = hero, true = glance (compressed cards)
	blink   bool   // caret / pulse phase
	focusID string // selected session id (focus follows it across refreshes)

	picker  bool
	query   string
	results []proj
	pCur    int
}

type proj struct {
	path   string // ~/-relativized for display
	abs    string
	branch string
	recent string // "2h", "" if not a recent
}

type tickMsg time.Time
type sessionsMsg struct {
	rows []session
	err  error
}
type attachDoneMsg struct{ err error }

func newConsole(db *sql.DB) consoleModel { return consoleModel{db: db} }

func (m consoleModel) Init() tea.Cmd { return tea.Batch(refresh(m.db), tick()) }

func tick() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refresh(db *sql.DB) tea.Cmd {
	return func() tea.Msg {
		rows, err := loadSessions(db)
		return sessionsMsg{rows, err}
	}
}

// ---- update ----

func (m consoleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tickMsg:
		m.blink = !m.blink
		return m, tea.Batch(refresh(m.db), tick())
	case sessionsMsg:
		m.rooms, m.err = groupRooms(msg.rows), msg.err
		if m.focusID == "" || m.findFocus() < 0 {
			m.focusFirst(false) // first tile; ! jumps to blocked
		}
	case attachDoneMsg:
		m.err = msg.err
	case tea.KeyMsg:
		if m.picker {
			return m.keyPicker(msg)
		}
		return m.keyDash(msg)
	}
	return m, nil
}

func (m consoleModel) keyDash(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "z":
		m.glance = !m.glance
	case "ctrl+n":
		m.picker, m.query, m.pCur = true, "", 0
		m.results = scanProjects("")
	case "tab", "right", "l", "down", "j":
		m.moveFocus(1)
	case "shift+tab", "left", "h", "up", "k":
		m.moveFocus(-1)
	case "!":
		m.focusFirst(true)
	case "enter":
		return m, m.attachFocused()
	}
	return m, nil
}

func (m consoleModel) keyPicker(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "ctrl+c":
		m.picker = false
	case "up":
		if m.pCur > 0 {
			m.pCur--
		}
	case "down":
		if m.pCur < len(m.results)-1 {
			m.pCur++
		}
	case "enter":
		if m.pCur < len(m.results) {
			dir := m.results[m.pCur].abs
			m.picker = false
			return m, func() tea.Msg { return attachDoneMsg{launchLead(dir)} }
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.results, m.pCur = scanProjects(m.query), 0
		}
	default:
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			m.query += string(k.Runes)
			if k.Type == tea.KeySpace {
				m.query += " "
			}
			m.results, m.pCur = scanProjects(m.query), 0
		}
	}
	return m, nil
}

// ---- focus ----

type pos struct{ g, t int }

func (m consoleModel) order() []pos {
	var out []pos
	for gi, r := range m.rooms {
		for ti := range r.agents {
			out = append(out, pos{gi, ti})
		}
	}
	return out
}

func (m consoleModel) findFocus() int {
	for i, p := range m.order() {
		if m.rooms[p.g].agents[p.t].ID == m.focusID {
			return i
		}
	}
	return -1
}

func (m *consoleModel) focusFirst(preferBlocked bool) {
	ord := m.order()
	if len(ord) == 0 {
		m.focusID = ""
		return
	}
	if preferBlocked {
		for _, p := range ord {
			if m.rooms[p.g].agents[p.t].Status == "blocked" {
				m.focusID = m.rooms[p.g].agents[p.t].ID
				return
			}
		}
	}
	p := ord[0]
	m.focusID = m.rooms[p.g].agents[p.t].ID
}

func (m *consoleModel) moveFocus(d int) {
	ord := m.order()
	if len(ord) == 0 {
		return
	}
	i := m.findFocus()
	if i < 0 {
		i = 0
	} else {
		i = (i + d + len(ord)) % len(ord)
	}
	p := ord[i]
	m.focusID = m.rooms[p.g].agents[p.t].ID
}

func (m consoleModel) focused() *session {
	if i := m.findFocus(); i >= 0 {
		p := m.order()[i]
		return &m.rooms[p.g].agents[p.t]
	}
	return nil
}

// attachFocused drops into the focused agent's live tmux window (the design's
// ⏎ expand) — lazygit's suspend/attach model. Detach returns to the dashboard;
// the agent keeps running throughout.
func (m consoleModel) attachFocused() tea.Cmd {
	a := m.focused()
	if a == nil || a.TmuxPane == "" {
		return nil
	}
	c := exec.Command("tmux", "select-window", "-t", a.TmuxPane, ";", "attach", "-t", tmuxSession)
	return tea.ExecProcess(c, func(err error) tea.Msg { return attachDoneMsg{err} })
}

// ---- counts ----

func (m consoleModel) counts() (groups, agents, blocked int) {
	groups = len(m.rooms)
	for _, r := range m.rooms {
		agents += len(r.agents)
		blocked += r.blocked()
	}
	return
}
