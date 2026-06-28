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
| `z` | glance mode (compressed keep-an-eye cards) |
| `!` | jump to a blocked agent |
| `q` | quit (the agents currently live in-process — see ADR-0003) |

## Status

Milestone 1 (current): the compositor + **live embedded Claude terminals** +
project picker + nav/insert focus. Next: status hooks → `state.db` (so
blocked/idle/done light up), the hcom inter-agent bus, dynamic `omni spawn` into
a group, and session persistence across restarts.
