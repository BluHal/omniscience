# Zellij as the tiling engine, omni as an external Go controller

We host each **Tile** (a live, interactive Claude **Session**) in a Zellij pane,
and drive Zellij from an external Go controller via `zellij action` + KDL
layouts — no WASM plugin. Tiles must run the real `claude` TUI (not a distilled
chat), so the engine has to do PTY, tiling, resize, scrollback and persistence;
building our own terminal emulator is exactly the hard 80%, and tmux gives less
native tiling polish than Zellij. A Go controller keeps the existing launch /
status-hook / hcom code; Zellij's richest custom chrome would need Rust WASM
plugins, which we are deferring.

## Status

Accepted. **Supersedes SPEC decisions 3** (tmux panes, "no terminal emulator")
**and 12** (read-only TILED view, raw output stays in tmux via `Enter → attach`).

## Considered options

- **Build our own embedded VT emulator** (Bubble Tea + `creack/pty` +
  `charmbracelet/x/vt`) — full control of the chrome, but reinvents a terminal
  multiplexer; rejected as too much risk for the core hard part.
- **tmux as engine** — already a dependency, free persistence, but weaker
  native tiling/chrome aesthetics.
- **Zellij + Rust WASM plugin** — best native chrome, but a Rust/WASM toolchain
  and a bilingual project; deferred (escalation path if Zellij's default chrome
  proves insufficient).

## Consequences

- The Bubble Tea dashboard *rendering* (`tui.go`'s views) is dropped; Zellij is
  the dashboard. The project picker and any control panels become small Go TUIs
  running inside Zellij (e.g. floating) panes that call `zellij action`.
- Chrome is bounded by Zellij layouts/themes/pane-frames until a Rust plugin is
  added.
- New runtime dependency: Zellij must be installed (currently it is not).
