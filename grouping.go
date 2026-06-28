package main

// grouping.go turns the flat state.db session list into rooms (the design's
// "groups"). A room is one Lead Session plus the Agents it spawned, sharing a
// bus; empty-room sessions collapse into the "(no room)" bucket. Pure and
// unit-tested (see tui_test.go) so the console can lean on a stable ordering.

// room is one navigable bucket of agents sharing a state.db room value.
type room struct {
	name   string
	agents []session
}

const noRoom = "(no room)"

// blocked reports how many agents in the room are blocked (the "needs me now"
// signal that floats the room to the front of the list).
func (r room) blocked() (n int) {
	for _, a := range r.agents {
		if a.Status == "blocked" {
			n++
		}
	}
	return n
}

// leadIndex picks the Lead within a room: the agent whose role is "lead", else
// the earliest-started (the root the others were spawned from). Returns 0 for an
// empty room. ponytail: heuristic until state.db tracks a parent/lead column.
func (r room) leadIndex() int {
	lead, best := 0, int64(1<<62)
	for i, a := range r.agents {
		if a.Role == "lead" {
			return i
		}
		if a.StartedAt < best {
			best, lead = a.StartedAt, i
		}
	}
	return lead
}

// groupRooms groups agents by room (empty → "(no room)"), floats blocked agents
// to the top within each room, and floats rooms with a blocked agent to the
// front. Self-contained so it's testable without loadSessions' ordering.
func groupRooms(sessions []session) []room {
	var rooms []room
	idx := map[string]int{}
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

// floatBlockedRooms stable-sorts rooms with a blocked agent to the front (n is
// tiny, insertion sort is plenty).
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
