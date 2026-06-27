package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// runTUI is the disposable dashboard: it polls state.db and renders live agent
// status. Closing it never touches the running agents (they live in tmux).
func runTUI() error {
	db, err := openDB(dbPath())
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(tuiModel{db: db}, tea.WithAltScreen()).Run()
	db.Close()
	return err
}

type tuiModel struct {
	db       *sql.DB
	sessions []session
	err      error
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
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case tickMsg:
		return m, tea.Batch(refresh(m.db), tick())
	case sessionsMsg:
		m.sessions, m.err = msg.rows, msg.err
	}
	return m, nil
}

func (m tuiModel) View() string {
	var b strings.Builder
	b.WriteString("  OMNISCIENCE\n\n")
	if m.err != nil {
		fmt.Fprintf(&b, "  error: %v\n\n", m.err)
	}
	if len(m.sessions) == 0 {
		b.WriteString("  no agents — launch with:  omni up <room>\n")
	}
	for _, s := range m.sessions {
		mark := " "
		if s.Status == "blocked" {
			mark = "●"
		}
		act := ""
		if s.CurrentActivity != "" {
			act = " · " + s.CurrentActivity
		}
		fmt.Fprintf(&b, "  %s %-12s %-10s %-8s%-18s %s\n",
			mark, s.Role, s.Room, s.Status, act, since(s.LastEventAt))
	}
	b.WriteString("\n  q quit\n")
	return b.String()
}
