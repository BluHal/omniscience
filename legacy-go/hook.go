package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"time"
)

// hook is invoked by Claude Code as `omni hook <event>`. Identity (which row,
// which db) arrives via env vars the launching claude process exported. It must
// never break the agent, so on any error it simply exits 0.
func hook(event string) {
	id := os.Getenv("OMNI_ID")
	dbp := os.Getenv("OMNI_DB")
	if id == "" || dbp == "" {
		os.Exit(0)
	}
	// Only PreToolUse needs stdin (to read which tool is about to run).
	tool := ""
	if event == "pre" {
		var in struct {
			ToolName string `json:"tool_name"`
		}
		_ = json.NewDecoder(os.Stdin).Decode(&in)
		tool = in.ToolName
	}
	db, err := openDB(dbp)
	if err != nil {
		os.Exit(0)
	}
	defer db.Close()
	_ = applyEvent(db, id, event, tool)
	os.Exit(0)
}

// applyEvent maps a hook event to a status/activity update on one row. Kept
// separate from hook() so it is unit-testable without spawning Claude.
func applyEvent(db *sql.DB, id, event, tool string) error {
	now := time.Now().Unix()
	switch event {
	case "sessionstart":
		_, err := db.Exec(`UPDATE sessions SET status='working', last_event_at=? WHERE id=?`, now, id)
		return err
	case "pre":
		_, err := db.Exec(`UPDATE sessions SET status='working', current_activity=?, last_event_at=? WHERE id=?`, tool, now, id)
		return err
	case "notify":
		_, err := db.Exec(`UPDATE sessions SET status='blocked', last_event_at=? WHERE id=?`, now, id)
		return err
	case "stop":
		_, err := db.Exec(`UPDATE sessions SET status='idle', current_activity='', last_event_at=? WHERE id=?`, now, id)
		return err
	case "end":
		_, err := db.Exec(`UPDATE sessions SET status='done', last_event_at=? WHERE id=?`, now, id)
		return err
	}
	return nil
}
