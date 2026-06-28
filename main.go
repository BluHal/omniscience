// Command omni is a terminal dashboard to launch and monitor parallel Claude
// Code agents. See SPEC.md. This is the first vertical slice (tracer bullet):
// `omni up <room>` spawns agents in a detached tmux session wired to status
// hooks that write ~/.omni/state.db; `omni` (no args) is the live dashboard.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		if err := runTUI(); err != nil {
			fmt.Fprintln(os.Stderr, "omni:", err)
			os.Exit(1)
		}
		return
	}
	switch os.Args[1] {
	case "up":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: omni up <room>")
			os.Exit(2)
		}
		if err := up(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "omni up:", err)
			os.Exit(1)
		}
	case "spawn":
		// omni spawn <room> <role> [brief...] — add one agent to a live room.
		if len(os.Args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: omni spawn <room> <role> [brief]")
			os.Exit(2)
		}
		brief := strings.Join(os.Args[4:], " ")
		if err := spawn(os.Args[2], os.Args[3], brief); err != nil {
			fmt.Fprintln(os.Stderr, "omni spawn:", err)
			os.Exit(1)
		}
	case "hook":
		// Invoked by Claude Code on every hook event. Must never break the
		// agent, so it always exits 0 (see hook()).
		if len(os.Args) < 3 {
			os.Exit(0)
		}
		hook(os.Args[2])
	default:
		fmt.Fprintln(os.Stderr, "usage: omni [up <room> | spawn <room> <role> [brief] | hook <event>]")
		os.Exit(2)
	}
}
