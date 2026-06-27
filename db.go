package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// session is one row of ~/.omni/state.db — the status of a single agent.
type session struct {
	ID, Room, ProjectPath, Role, TmuxPane, Model, Status, CurrentActivity string
	StartedAt, LastEventAt                                                 int64
}

// dbPath resolves the state.db location. Hook subprocesses get it via OMNI_DB
// (inherited from the launching claude process); everything else defaults to
// the fixed ~/.omni/state.db.
func dbPath() string {
	if p := os.Getenv("OMNI_DB"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".omni", "state.db")
}

// openDB opens (creating if needed) the state.db with WAL + a busy timeout, so
// the many concurrent hook-process writers and the TUI reader don't collide.
func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions(
		id TEXT PRIMARY KEY, room TEXT, project_path TEXT, role TEXT,
		tmux_pane TEXT, model TEXT, status TEXT, current_activity TEXT,
		started_at INTEGER, last_event_at INTEGER)`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// loadSessions returns all rows with blocked agents floated to the top (the
// "needs me now" signal), then most-recent activity first.
func loadSessions(db *sql.DB) ([]session, error) {
	rows, err := db.Query(`SELECT id,room,project_path,role,tmux_pane,model,status,current_activity,started_at,last_event_at FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session
	for rows.Next() {
		var s session
		if err := rows.Scan(&s.ID, &s.Room, &s.ProjectPath, &s.Role, &s.TmuxPane,
			&s.Model, &s.Status, &s.CurrentActivity, &s.StartedAt, &s.LastEventAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		bi, bj := out[i].Status == "blocked", out[j].Status == "blocked"
		if bi != bj {
			return bi
		}
		return out[i].LastEventAt > out[j].LastEventAt
	})
	return out, rows.Err()
}

func since(ts int64) string {
	if ts == 0 {
		return "-"
	}
	return time.Since(time.Unix(ts, 0)).Round(time.Second).String()
}
