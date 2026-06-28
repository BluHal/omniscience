package main

import (
	"strings"
	"testing"
)

// TestViewRendersRoomsAndChat is the runnable check that the rooms list and the
// in-room TILED view assemble correctly end to end (issues #1/#3/#6) — no db,
// tmux, or claude. It asserts the blocked indicator, the (no room) bucket, the
// per-agent columns, the ◆ broadcast marker that distinguishes a decision from a
// direct message, and the compose line.
func TestViewRendersRoomsAndChat(t *testing.T) {
	m := tuiModel{
		width: 120,
		rooms: groupRooms([]session{
			{ID: "1", Room: "feature-x", Role: "frontend", Status: "blocked", CurrentActivity: "Edit", ProjectPath: "/tmp/p"},
			{ID: "2", Room: "feature-x", Role: "backend", Status: "working", ProjectPath: "/tmp/p"},
			{ID: "3", Room: "", Role: "solo", Status: "idle"},
		}),
	}

	// Rooms list: blocked count + the (no room) bucket both surface.
	list := m.View()
	for _, want := range []string{"feature-x", "● 1 blocked", noRoom} {
		if !strings.Contains(list, want) {
			t.Errorf("rooms list missing %q:\n%s", want, list)
		}
	}

	// Open feature-x with a broadcast and a direct on the bus.
	m.inRoom = true
	m.chat = []chatEntry{
		{from: "backend", text: "migrations done", broadcast: true},
		{from: "backend", text: "JWT or opaque?", toRoles: []string{"frontend"}},
	}
	room := m.View()
	for _, want := range []string{
		"room: feature-x",
		"frontend", "backend", // both agent columns
		"◆ backend: migrations done", // broadcast marked as a decision
		"backend: JWT or opaque?",    // direct shows for the involved agents
		"→ frontend:",                // compose line targets the focused agent
	} {
		if !strings.Contains(room, want) {
			t.Errorf("open room missing %q:\n%s", want, room)
		}
	}
}
