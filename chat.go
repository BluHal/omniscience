package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// chatEntry is one hcom bus message with its hcom identities resolved to omni
// roles (the dashboard's column keys). broadcast distinguishes a room-wide
// decision from a direct message (SPEC decision 11).
type chatEntry struct {
	from      string   // resolved sender role (or raw hcom name if unresolved)
	text      string
	broadcast bool     // room-wide decision broadcast vs a direct message
	toRoles   []string // resolved recipient roles (direct messages only)
}

// roomHcomDir returns a room's isolated hcom bus directory (HCOM_DIR), or "" if
// the room has no bus — the "(no room)" bucket (monitored-but-mute) or an empty
// room. Each room's agents share a project_path; the bus is co-located under it.
func roomHcomDir(r room) string {
	if r.name == noRoom || len(r.agents) == 0 {
		return ""
	}
	return filepath.Join(r.agents[0].ProjectPath, ".omni", r.name, ".hcom")
}

// roomHcomDB returns the path to a room's hcom message db, or "" if no bus.
func roomHcomDB(r room) string {
	if d := roomHcomDir(r); d != "" {
		return filepath.Join(d, "hcom.db")
	}
	return ""
}

// loadChat reads a room's hcom messages, resolving each hcom identity to its
// omni role via the bus's instances table (tag=role, set by HCOM_TAG at launch).
// Read-only and resilient: a missing or busy db just yields no messages, never
// an error that would break the poll loop.
func loadChat(path string) ([]chatEntry, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil, nil // bus db isn't created until an agent joins — not an error
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(2000)&mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	resolve := nameResolver(db)
	rows, err := db.Query(`SELECT msg_from, COALESCE(msg_text,''), COALESCE(msg_scope,''),
		COALESCE(msg_delivered_to,'[]'), COALESCE(msg_mentions,'[]')
		FROM events_v WHERE type='message' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []chatEntry
	for rows.Next() {
		var from, text, scope, deliv, ment string
		if err := rows.Scan(&from, &text, &scope, &deliv, &ment); err != nil {
			return nil, err
		}
		e := chatEntry{from: resolve(from), text: text, broadcast: scope == "broadcast"}
		seen := map[string]bool{}
		for _, n := range append(jsonNames(deliv), jsonNames(ment)...) {
			r := resolve(n)
			if r != "" && !seen[r] {
				seen[r] = true
				e.toRoles = append(e.toRoles, r)
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// nameResolver maps an hcom identity to an omni role. The room launch tags each
// agent with HCOM_TAG=<role>, so instances.tag is the role; if the tag wasn't
// recorded, fall back to the base name before the first '-' (hcom full names are
// "<tag>-<base>"), else the name verbatim.
func nameResolver(db *sql.DB) func(string) string {
	nameRole := map[string]string{}
	if rows, err := db.Query(`SELECT name, COALESCE(tag,'') FROM instances`); err == nil {
		for rows.Next() {
			var name, tag string
			if rows.Scan(&name, &tag) == nil && tag != "" {
				nameRole[name] = tag
			}
		}
		rows.Close()
	}
	return func(name string) string {
		if r, ok := nameRole[name]; ok {
			return r
		}
		if i := strings.IndexByte(name, '-'); i > 0 {
			return name[:i]
		}
		return name
	}
}

// resolveAgentName returns the exact hcom identity to address for an omni role
// on a room's bus, or "" if no joined agent matches (not started yet). Reads the
// instances table so a direct send hits the agent whether its role rode in on
// the name (hcom start --as <role>) or the tag (HCOM_TAG=<role>).
func resolveAgentName(hcomDir, role string) string {
	path := filepath.Join(hcomDir, "hcom.db")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(2000)&mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name, COALESCE(tag,'') FROM instances`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var name, tag string
		if rows.Scan(&name, &tag) != nil {
			continue
		}
		if name == role || tag == role || (tag == "" && strings.HasPrefix(name, role+"-")) {
			return name
		}
	}
	return ""
}

func jsonNames(s string) []string {
	var a []string
	_ = json.Unmarshal([]byte(s), &a)
	return a
}

// chatFor returns the messages belonging to one agent's column: every broadcast
// (the whole room sees a decision) plus directs authored by or addressed to that
// role. Order is preserved from loadChat (chronological).
func chatFor(all []chatEntry, role string) []chatEntry {
	var out []chatEntry
	for _, e := range all {
		if e.broadcast || e.from == role {
			out = append(out, e)
			continue
		}
		for _, t := range e.toRoles {
			if t == role {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
