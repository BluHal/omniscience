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
}

type tickMsg time.Time
type sessionsMsg struct {
	rows []session
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

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.inRoom {
				m.agtCur--
			} else {
				m.roomCur--
			}
		case "down", "j":
			if m.inRoom {
				m.agtCur++
			} else {
				m.roomCur++
			}
		case "enter", "l", "right":
			if !m.inRoom && m.roomCur < len(m.rooms) && len(m.rooms[m.roomCur].agents) > 0 {
				m.inRoom, m.agtCur = true, 0
			}
		case "esc", "h", "left":
			m.inRoom = false
		}
		m.clamp()
	case tickMsg:
		return m, tea.Batch(refresh(m.db), tick())
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

func (m tuiModel) viewRoom(b *strings.Builder) {
	r := m.rooms[m.roomCur]
	fmt.Fprintf(b, "  room: %s\n\n", r.name)
	for i, a := range r.agents {
		cur := " "
		if i == m.agtCur {
			cur = ">"
		}
		mark := " "
		if a.Status == "blocked" {
			mark = "●"
		}
		act := ""
		if a.CurrentActivity != "" {
			act = " · " + a.CurrentActivity
		}
		fmt.Fprintf(b, "  %s %s %-12s %-8s%-18s %s\n",
			cur, mark, a.Role, a.Status, act, since(a.LastEventAt))
	}
	b.WriteString("\n  ↑↓/jk move · esc back · q quit\n")
}
