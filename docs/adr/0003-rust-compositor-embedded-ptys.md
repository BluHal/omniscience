# omni is a bespoke Rust compositor that owns the live terminals

omni is rewritten in **Rust (ratatui)** as a bespoke TUI compositor that draws
the entire Claude Design screen itself and **owns the live Claude sessions**:
each tile is a real PTY (`portable-pty`) whose output is parsed by `vt100` and
rendered with `tui-term`; keystrokes route to the focused tile (nav/insert
modes). This is what makes a tile a genuine, interactive embedded terminal —
not a status peek or a polled mirror.

## Status

Accepted. **Supersedes ADR-0001** (Zellij as engine + Go controller) and retires
the Go implementation (preserved in git history).

## Why

- The design is a fully bespoke compositor — custom group frames with floating
  labels, double-border focus, blocked-red treatment — which Zellij's default
  chrome cannot produce without a Rust WASM plugin. So we draw it ourselves.
- The load-bearing feature is embedding Claude's full interactive TUI inside a
  tile. Rust has the proven stack for exactly this: `ratatui` + `tui-term` +
  `portable-pty` + a `vt100`/wezterm-class VT engine. The Go path would have
  meant either an immature emulator (`charmbracelet/x/vt`) or a `tmux
  capture-pane` mirror (polled, laggy input via `send-keys`) — both worse.

## Consequences

- **Persistence (SPEC dec. 10) regresses for now:** omni owns the PTYs
  in-process, so closing omni ends the sessions. A detach/daemon split (PTY host
  + TUI client) is a follow-up if persistence is wanted back.
- Status hooks → `state.db` (so blocked/idle/done colours light up) and the hcom
  inter-agent bus are **not yet ported**; tiles currently show a working/live
  status. These are the next milestones.
- New runtime deps: a Rust toolchain; `omni` is installed via `cargo install`.
