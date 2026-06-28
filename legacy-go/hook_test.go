package main

import (
	"path/filepath"
	"testing"
)

// TestApplyEvent is the runnable check behind the hook→db logic: each event
// must land the right status/activity on the row, without spawning Claude.
func TestApplyEvent(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO sessions(id,status,current_activity) VALUES('x','starting','')`); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ event, tool, wantStatus, wantAct string }{
		{"sessionstart", "", "working", ""},
		{"pre", "Bash", "working", "Bash"},
		{"notify", "", "blocked", "Bash"}, // activity persists while blocked
		{"stop", "", "idle", ""},          // idle clears activity
		{"end", "", "done", ""},
	}
	for _, c := range cases {
		if err := applyEvent(db, "x", c.event, c.tool); err != nil {
			t.Fatalf("%s: %v", c.event, err)
		}
		var status, act string
		if err := db.QueryRow(`SELECT status,current_activity FROM sessions WHERE id='x'`).Scan(&status, &act); err != nil {
			t.Fatalf("%s: scan: %v", c.event, err)
		}
		if status != c.wantStatus || act != c.wantAct {
			t.Errorf("%s: got (%s,%q) want (%s,%q)", c.event, status, act, c.wantStatus, c.wantAct)
		}
	}
}
