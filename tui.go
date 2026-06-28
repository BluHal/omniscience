package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// runTUI is the disposable dashboard: it polls state.db and renders live agent
// status grouped into rooms. Closing it never touches the running agents (they
// live in tmux).
func runTUI() error {
	db, err := openDB(dbPath())
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(tuiModel{db: db}, tea.WithAltScreen()).Run()
	db.Close()
	return err
}

// room is one navigable bucket of agents sharing a state.db room value. The
// empty room becomes the "(no room)" pseudo-room (monitored-but-mute agents).
type room struct {
	name   string
	agents []session
}

const noRoom = "(no room)"

// blocked reports whether any agent in the room is blocked (the "needs me now"
// signal that floats the room to the top of the list).
func (r room) blocked() (n int) {
	for _, a := range r.agents {
		if a.Status == "blocked" {
			n++
		}
	}
	return n
}

// groupRooms turns the flat session list into ordered rooms: agents grouped by
// room (empty → "(no room)"), blocked agents floated to the top within each
// room, and rooms containing a blocked agent floated to the top of the list.
// Self-contained (doesn't lean on loadSessions' ordering) so it's unit-testable.
func groupRooms(sessions []session) []room {
	var rooms []room
	idx := map[string]int{} // room name -> position in rooms
	for _, s := range sessions {
		name := s.Room
		if name == "" {
			name = noRoom
		}
		i, ok := idx[name]
		if !ok {
			i = len(rooms)
			idx[name] = i
			rooms = append(rooms, room{name: name})
		}
		rooms[i].agents = append(rooms[i].agents, s)
	}
	for i := range rooms {
		floatBlockedAgents(rooms[i].agents)
	}
	floatBlockedRooms(rooms)
	return rooms
}

// floatBlockedRooms stable-sorts rooms with a blocked agent to the front. n is
// tiny, so insertion sort is plenty.
func floatBlockedRooms(rooms []room) {
	for i := 1; i < len(rooms); i++ {
		for j := i; j > 0 && rooms[j].blocked() > 0 && rooms[j-1].blocked() == 0; j-- {
			rooms[j], rooms[j-1] = rooms[j-1], rooms[j]
		}
	}
}

// floatBlockedAgents stable-sorts blocked agents to the front of a room.
func floatBlockedAgents(agents []session) {
	for i := 1; i < len(agents); i++ {
		for j := i; j > 0 && agents[j].Status == "blocked" && agents[j-1].Status != "blocked"; j-- {
			agents[j], agents[j-1] = agents[j-1], agents[j]
		}
	}
}

type tuiModel struct {
	db    *sql.DB
	rooms []room
	err   error

	inRoom  bool
	roomCur int
	agtCur  int

	width, height int
	chat          []chatEntry // open room's hcom messages (read live)
	chatErr       error
}

type tickMsg time.Time
type sessionsMsg struct {
	rows []session
	err  error
}
type chatLoadedMsg struct {
	rows []chatEntry
	err  error
}

func (m tuiModel) Init() tea.Cmd { return tea.Batch(refresh(m.db), tick()) }

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refresh(db *sql.DB) tea.Cmd {
	return func() tea.Msg {
		rows, err := loadSessions(db)
		return sessionsMsg{rows, err}
	}
}

func loadChatCmd(path string) tea.Cmd {
	return func() tea.Msg {
		rows, err := loadChat(path)
		return chatLoadedMsg{rows, err}
	}
}

// openHcomDB is the bus db of the currently open room, or "" if none is open or
// the room has no bus (drives the live chat poll).
func (m tuiModel) openHcomDB() string {
	if !m.inRoom || m.roomCur >= len(m.rooms) {
		return ""
	}
	return roomHcomDB(m.rooms[m.roomCur])
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.inRoom {
			return m.keyInRoom(msg)
		}
		return m.keyRooms(msg)
	case tickMsg:
		// The chat poll rides the same tick as the status poll, so the TILED
		// view refreshes live as agents post to the bus.
		cmds := []tea.Cmd{refresh(m.db), tick()}
		if p := m.openHcomDB(); p != "" {
			cmds = append(cmds, loadChatCmd(p))
		}
		return m, tea.Batch(cmds...)
	case chatLoadedMsg:
		m.chat, m.chatErr = msg.rows, msg.err
	case sessionsMsg:
		m.err = msg.err
		// Keep selection stable across the regroup: remember what's selected,
		// rebuild, then re-find it (rooms can reorder/empty between ticks).
		var selRoom, selAgent string
		if m.roomCur < len(m.rooms) {
			selRoom = m.rooms[m.roomCur].name
			if m.agtCur < len(m.rooms[m.roomCur].agents) {
				selAgent = m.rooms[m.roomCur].agents[m.agtCur].ID
			}
		}
		m.rooms = groupRooms(msg.rows)
		m.restore(selRoom, selAgent)
		m.clamp()
	}
	return m, nil
}

// keyRooms handles keys on the rooms list: navigate and open a room.
func (m tuiModel) keyRooms(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.roomCur--
	case "down", "j":
		m.roomCur++
	case "enter", "l", "right":
		if m.roomCur < len(m.rooms) && len(m.rooms[m.roomCur].agents) > 0 {
			m.inRoom, m.agtCur = true, 0
			m.chat, m.chatErr = nil, nil // clear stale chat; tick repopulates
			return m, loadChatCmd(m.openHcomDB())
		}
	}
	m.clamp()
	return m, nil
}

// keyInRoom handles keys while a room is open. Focus moves between agent columns
// with the arrows; esc returns to the rooms list. (Compose/send #6 and tmux
// attach #4 add their keys here.)
func (m tuiModel) keyInRoom(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.inRoom = false
		m.chat, m.chatErr = nil, nil
	case "up", "left":
		m.agtCur--
	case "down", "right":
		m.agtCur++
	}
	m.clamp()
	return m, nil
}

// restore re-points the cursors at the previously selected room/agent by name
// and id, so a refresh that reorders rooms doesn't jump the selection.
func (m *tuiModel) restore(selRoom, selAgent string) {
	for i, r := range m.rooms {
		if r.name != selRoom {
			continue
		}
		m.roomCur = i
		for j, a := range r.agents {
			if a.ID == selAgent {
				m.agtCur = j
			}
		}
		return
	}
}

// clamp keeps the cursors in range so an emptied/closed room never panics.
func (m *tuiModel) clamp() {
	if m.roomCur < 0 {
		m.roomCur = 0
	}
	if m.roomCur >= len(m.rooms) {
		m.roomCur = len(m.rooms) - 1
	}
	if m.roomCur < 0 || len(m.rooms) == 0 {
		m.inRoom, m.roomCur, m.agtCur = false, 0, 0
		return
	}
	n := len(m.rooms[m.roomCur].agents)
	if m.agtCur < 0 {
		m.agtCur = 0
	}
	if m.agtCur >= n {
		m.agtCur = n - 1
	}
	if m.agtCur < 0 {
		m.agtCur = 0
	}
}

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString("  OMNISCIENCE\n\n")
	if m.err != nil {
		fmt.Fprintf(&b, "  error: %v\n\n", m.err)
	}
	if len(m.rooms) == 0 {
		b.WriteString("  no agents — launch with:  omni up <room>\n")
		b.WriteString("\n  q quit\n")
		return b.String()
	}
	if m.inRoom {
		m.viewRoom(&b)
	} else {
		m.viewRooms(&b)
	}
	return b.String()
}

func (m tuiModel) viewRooms(b *strings.Builder) {
	for i, r := range m.rooms {
		cur := " "
		if i == m.roomCur {
			cur = ">"
		}
		blk := ""
		if n := r.blocked(); n > 0 {
			blk = fmt.Sprintf("  ● %d blocked", n)
		}
		fmt.Fprintf(b, "  %s %-20s %d agent(s)%s\n", cur, r.name, len(r.agents), blk)
	}
	b.WriteString("\n  ↑↓/jk move · enter open · q quit\n")
}

// viewRoom renders the open room TILED (SPEC decision 12): one column per agent,
// side by side. Each column header carries the agent's status/activity/last-event
// age (issue #1); below it, that agent's hcom messages (issue #3). The focused
// column is bracketed.
func (m tuiModel) viewRoom(b *strings.Builder) {
	r := m.rooms[m.roomCur]
	fmt.Fprintf(b, "  room: %s\n", r.name)
	if m.chatErr != nil {
		fmt.Fprintf(b, "  chat error: %v\n", m.chatErr)
	}
	b.WriteString("\n")

	const msgRows = 8
	n := len(r.agents)
	colW := m.colWidth(n)
	cols := make([][]string, n)
	for i, a := range r.agents {
		head := a.Role
		if i == m.agtCur {
			head = "[" + a.Role + "]"
		}
		mark := "  "
		if a.Status == "blocked" {
			mark = "● "
		}
		meta := a.Status + " · " + since(a.LastEventAt)
		if a.CurrentActivity != "" {
			meta = a.Status + " · " + a.CurrentActivity
		}
		col := []string{
			truncPad(mark+head, colW),
			truncPad("  "+meta, colW),
			strings.Repeat("─", colW),
		}
		col = append(col, m.chatLines(r, a, colW, msgRows)...)
		cols[i] = col
	}

	rowsN := 0
	for _, c := range cols {
		if len(c) > rowsN {
			rowsN = len(c)
		}
	}
	for row := 0; row < rowsN; row++ {
		b.WriteString("  ")
		for _, c := range cols {
			cell := strings.Repeat(" ", colW)
			if row < len(c) {
				cell = c[row]
			}
			b.WriteString(cell + " │ ")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n  ←→/↑↓ focus agent · esc back · q quit\n")
}

// chatLines renders one column's message body: broadcasts (◆) and directs for
// this agent, last msgRows of them, each padded to the column width. A no-bus
// room or an empty thread renders a single clean placeholder.
func (m tuiModel) chatLines(r room, a session, colW, msgRows int) []string {
	if r.name == noRoom {
		return []string{truncPad("(no bus)", colW)}
	}
	msgs := chatFor(m.chat, a.Role)
	if len(msgs) == 0 {
		return []string{truncPad("—", colW)}
	}
	if len(msgs) > msgRows {
		msgs = msgs[len(msgs)-msgRows:]
	}
	out := make([]string, 0, len(msgs))
	for _, e := range msgs {
		prefix := e.from + ": "
		if e.broadcast {
			prefix = "◆ " + e.from + ": " // decision broadcast marker (issue #6)
		}
		out = append(out, truncPad(prefix+e.text, colW))
	}
	return out
}

// colWidth splits the terminal across n agent columns, clamped to a readable
// range. Falls back to a sane width before the first WindowSizeMsg.
func (m tuiModel) colWidth(n int) int {
	w := m.width
	if w <= 0 {
		w = 100
	}
	colW := (w-2)/n - 3
	if colW < 16 {
		colW = 16
	}
	if colW > 40 {
		colW = 40
	}
	return colW
}

// truncPad fits s to exactly w runes: padded with spaces or truncated with an
// ellipsis. Plain text only (no inline ANSI) so column alignment survives.
func truncPad(s string, w int) string {
	r := []rune(s)
	if len(r) == w {
		return s
	}
	if len(r) < w {
		return s + strings.Repeat(" ", w-len(r))
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}
