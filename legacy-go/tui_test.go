package main

import "testing"

// TestGroupRooms covers the three load-bearing grouping rules. Input order
// mimics loadSessions output (blocked floated to top within the flat list).
func TestGroupRooms(t *testing.T) {
	in := []session{
		{ID: "1", Room: "alpha", Status: "blocked"}, // blocked → floats alpha up
		{ID: "2", Room: "beta", Status: "working"},
		{ID: "3", Room: "beta", Status: "blocked"}, // blocked in beta
		{ID: "4", Room: "", Status: "working"},     // no room → "(no room)"
		{ID: "5", Room: "", Status: "idle"},
	}
	rooms := groupRooms(in)

	// (a) a room with a blocked agent sorts to the top.
	if rooms[0].name != "alpha" {
		t.Fatalf("blocked room should float to top, got %q", rooms[0].name)
	}

	// (c) empty-room sessions land in a single (no room) bucket.
	var nr *room
	for i := range rooms {
		if rooms[i].name == noRoom {
			if nr != nil {
				t.Fatalf("(no room) split into multiple buckets")
			}
			nr = &rooms[i]
		}
	}
	if nr == nil || len(nr.agents) != 2 {
		t.Fatalf("(no room) bucket = %v, want 2 agents", nr)
	}

	// (b) within beta, the blocked agent floats to the top.
	var beta *room
	for i := range rooms {
		if rooms[i].name == "beta" {
			beta = &rooms[i]
		}
	}
	if beta == nil || beta.agents[0].ID != "3" {
		t.Fatalf("blocked agent should float to top of beta, got %+v", beta)
	}
}
