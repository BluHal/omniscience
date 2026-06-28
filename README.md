# Omniscience (`omni`)

> One all-seeing view over every Claude Code agent, everywhere.

A lazygit-style terminal dashboard to launch, monitor, and steer parallel
Claude Code agents across projects — with isolated **rooms** where agents on one
feature chat with each other (via [hcom](https://github.com/aannoo/hcom)) while
unrelated projects stay separate.

```
  OMNISCIENCE

  ● backend      feature-x  blocked                      4s
    frontend     feature-x  working   · Edit             1s
    api          payments   working   · Bash             9s
    docs                    idle                          3m

  q quit
```

*(`●` = blocked, needs you now — floats to the top. No room = monitored but mute.)*

## Why

The native tools each cover a slice — [Agent View](https://code.claude.com/docs/en/agent-view)
unifies one orchestration, [Agent Teams](https://code.claude.com/docs/en/agent-teams)
gives a message bus, [claude-squad](https://github.com/smtg-ai/claude-squad) /
[VibeMux](https://github.com/UgOrange/vibemux) wrap sessions in a TUI — but none
gives *one* cross-project view where agents on a shared feature collaborate and
everything else stays isolated. That's the wedge. The bus is reused (hcom); the
dashboard is the thing being built. Full landscape and rationale: **[SPEC.md](SPEC.md)**.

## Highlights

- **Cross-project overview** — every agent you launched, live status in one screen.
- **Blocked-agent triage** — `Notification` hooks float "needs me now" to the top.
- **Rooms** — a folder of briefs becomes a team of agents on an isolated bus;
  agents in different rooms (or no room) can't talk to each other.
- **Disposable viewer** — the dashboard is just a reader of `state.db`. Close it
  or crash it; agents keep running in a detached tmux session. Reopen to re-attach.

## Status

`omni up <room>` and `omni spawn <room> <role> [brief]` spawn agents wired to
status hooks (`~/.omni/state.db`) **and** each room's isolated hcom bus. `omni`
(no args) is the live dashboard: a room-grouped overview, an in-room TILED chat
read off the bus, Enter-to-attach into an agent's tmux, and send/broadcast back
to agents. See [SPEC.md](SPEC.md) for the design and remaining § Deferred items.

## Prerequisites

Go 1.26+, `tmux`, an authenticated `claude` CLI, and
[`hcom`](https://github.com/aannoo/hcom) (`brew install aannoo/hcom/hcom`) for
the per-room message bus.

## Build

```
go build -o omni .
```

## Use

A room is a folder of brief markdown files — one agent per file, `role` = filename,
brief = the file's contents:

```
.omni/feature-x/
  frontend.md
  backend.md
```

```
omni up feature-x                 # spawn one claude agent per brief on an isolated hcom bus
omni spawn feature-x reviewer      # add one more agent to the live room (optional brief: path or inline)
omni                              # live dashboard
tmux attach -t omni              # drop into the agents directly
```

In the dashboard: `↑↓` move, `enter` opens a room. Inside a room the agents
render TILED (each one's chat side-by-side): `←→` focus an agent, type + `enter`
sends direct to it, `ctrl+b` broadcasts to the whole room as a tagged decision,
`enter` on an empty line attaches to that agent's tmux, `esc` backs out.

## How it works

```
  omni TUI (Go + Bubble Tea)         standalone, disposable
        │ reads
        ▼
  ~/.omni/state.db  ◀── omni status hooks ──  agents in detached tmux session "omni"
```

`omni up` launches a plain (user-authed) `claude` per brief in its own tmux
window, injecting omni's status hooks via `--settings` so your global/project
config is never touched. Each hook event updates one row of `state.db`:

| Hook | Effect on the agent's row |
|---|---|
| `SessionStart` | `working` |
| `PreToolUse` | `current_activity = <tool>`, `working` |
| `Notification` | `blocked` — the "needs me now" signal |
| `Stop` | `idle` |
| `SessionEnd` | `done` |

The TUI is a 500ms poll over that one table — its entire cross-project view is a
single query. Opening a room adds a second poll over that room's hcom db
(`.omni/<room>/.hcom`) for the TILED chat; sending shells out to `hcom send`.
Architecture diagram and the per-room chat layer: **[SPEC.md](SPEC.md)**.
