# Omniscience — `omni`

A terminal dashboard to **run and watch many live Claude Code sessions side by
side**, across the same or different projects, with isolated groups where agents
collaborate. One all-seeing view over every agent, everywhere.

Each tile is a **real, interactive embedded terminal** running Claude — you see
its native TUI and type into it. The dashboard draws the whole screen itself
(bespoke compositor) following the Claude Design "Terminal Dashboard UI System".

See [SPEC.md](SPEC.md) for the product, [CONTEXT.md](CONTEXT.md) for the
glossary, and [docs/adr/](docs/adr/) for the architecture decisions. The
previous Go implementation is kept under [legacy-go/](legacy-go/) for reference.

## Stack

Rust — [ratatui](https://github.com/ratatui/ratatui) (chrome) ·
[tui-term](https://github.com/a-kenji/tui-term) +
[vt100](https://github.com/doy/vt100-rust) (terminal emulation) ·
[portable-pty](https://docs.rs/portable-pty) (PTYs) ·
[rusqlite](https://github.com/rusqlite/rusqlite) (state).

## Build & run

```sh
cargo install --path .   # puts `omni` on your PATH (~/.cargo/bin)
omni                     # open the dashboard (run it in a real terminal)
```

## Keys

| Key | Action |
|-----|--------|
| `^n` | new project — fuzzy picker over `~/work ~/src ~/dev`, opens Claude in a tile |
| `i` / `⏎` | type into the focused tile (insert mode) |
| `^\` | back to nav mode |
| `↹` / arrows | move focus between tiles |
| `^b` | broadcast a decision to the focused tile's group (type, ⏎ to send) |
| `z` | glance mode (compressed keep-an-eye cards) |
| `!` | jump to a blocked agent |
| `q` | quit (the agents currently live in-process — see ADR-0003) |

A Lead can grow its group at runtime: it's launched knowing it can run
`omni spawn <room> <role> [brief]`, which the dashboard picks up and opens as a
new tile in that group, on the same hcom bus.

## Status

Working: the compositor + **live embedded Claude terminals**, project picker,
nav/insert focus, **status** (working/blocked/idle/done via Claude hooks →
`state.db`, so blocked agents float up and light red), the **hcom** bus
(per-group, agents auto-join), **broadcast** (`^b`), and dynamic **`omni
spawn`** into a group.

Known gap: **persistence across restarts** — omni owns the PTYs in-process, so
quitting ends the sessions. A detach/daemon split (PTY host + TUI client) is the
remaining follow-up (see [ADR-0003](docs/adr/0003-rust-compositor-embedded-ptys.md)).
