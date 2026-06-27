package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildSettings is the runnable check behind the omni+hcom hook merge: the
// single --settings JSON must carry omni's status hooks, hcom's bus hooks for
// shared events, and the allow-list that pre-approves `hcom start`/`send`.
// Shells the real `hcom hooks add` into a temp dir (fast); no claude/tmux.
func TestBuildSettings(t *testing.T) {
	hcom, err := hcomSettings()
	if err != nil {
		t.Skipf("hcom not available: %v", err)
	}
	merged := buildSettings("/usr/local/bin/omni", hcom)
	b, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)

	for _, want := range []string{
		"/usr/local/bin/omni hook sessionstart", // (a) omni status hooks
		"/usr/local/bin/omni hook stop",
		"exec $cmd sessionstart", // (b) hcom bus hooks for a shared event
		"exec $cmd poll",         // hcom's Stop verb
		"Bash(hcom start:*)",     // (c) pre-approved self-join
		"Bash(hcom send:*)",      // pre-approved send
	} {
		if !strings.Contains(js, want) {
			t.Errorf("merged settings missing %q\n%s", want, js)
		}
	}

	// SessionStart must keep BOTH omni's and hcom's entries (concatenated).
	hooks := merged["hooks"].(map[string]any)
	ss := hooks["SessionStart"].([]any)
	if len(ss) < 2 {
		t.Errorf("SessionStart should carry both omni+hcom entries, got %d", len(ss))
	}
}
