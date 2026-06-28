package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestResolveAgentName checks the send-target resolution against a bus's
// instances table: a direct send must hit the right agent whether its role rode
// in on the name (hcom start --as <role>), the tag (HCOM_TAG=<role>), or only as
// a "<role>-<base>" name; and yield "" when the agent hasn't joined.
func TestResolveAgentName(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "hcom.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE instances(name TEXT, tag TEXT);
		INSERT INTO instances VALUES('frontend','frontend');
		INSERT INTO instances VALUES('vox','backend');
		INSERT INTO instances VALUES('worker-7','');`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	cases := []struct{ role, want string }{
		{"frontend", "frontend"}, // name==role (and tag too)
		{"backend", "vox"},       // tag==role, random name
		{"worker", "worker-7"},   // untagged, "<role>-<base>" prefix
		{"nobody", ""},           // not joined
	}
	for _, c := range cases {
		if got := resolveAgentName(dir, c.role); got != c.want {
			t.Errorf("resolveAgentName(%q) = %q, want %q", c.role, got, c.want)
		}
	}
	if got := resolveAgentName(filepath.Join(dir, "missing"), "frontend"); got != "" {
		t.Errorf("missing bus should resolve to \"\", got %q", got)
	}
}

// TestChatFor is the runnable check behind the TILED attribution: a column shows
// broadcasts (every agent) plus directs authored by or addressed to that agent,
// and nothing else.
func TestChatFor(t *testing.T) {
	all := []chatEntry{
		{from: "frontend", text: "scaffolding", broadcast: false},                // authored by FE
		{from: "backend", text: "JWT or opaque?", toRoles: []string{"frontend"}}, // BE -> FE direct
		{from: "omni", text: "use JWT", toRoles: []string{"backend"}},            // you -> BE direct
		{from: "backend", text: "migrations done", broadcast: true},              // broadcast (all)
	}

	fe := chatFor(all, "frontend")
	if len(fe) != 3 { // authored + BE->FE + broadcast
		t.Fatalf("frontend column: got %d msgs, want 3: %+v", len(fe), fe)
	}
	be := chatFor(all, "backend")
	if len(be) != 3 { // BE->FE authored + omni->BE + own broadcast
		t.Fatalf("backend column: got %d msgs, want 3: %+v", len(be), be)
	}
	// The omni->backend direct must NOT leak into the frontend column.
	for _, e := range fe {
		if e.text == "use JWT" {
			t.Errorf("frontend column leaked a direct meant for backend")
		}
	}
}

// TestJSONNames covers the hcom recipient-list parse (JSON array text -> names).
func TestJSONNames(t *testing.T) {
	got := jsonNames(`["frontend-luna","backend-vox"]`)
	if len(got) != 2 || got[0] != "frontend-luna" || got[1] != "backend-vox" {
		t.Fatalf("jsonNames: %v", got)
	}
	if n := jsonNames("[]"); len(n) != 0 {
		t.Fatalf("empty: %v", n)
	}
	if n := jsonNames(""); len(n) != 0 {
		t.Fatalf("invalid should yield none: %v", n)
	}
}
