<div align="center">

# Omniscience

**One all-seeing view over every Claude Code agent, everywhere.**

A terminal dashboard that runs and watches many **live, interactive Claude Code
sessions side by side** — across the same or different projects — with isolated
groups where agents collaborate over a shared message bus.

Each tile is a *real embedded terminal*: you see Claude's native TUI and type
straight into it. No transcript scraping, no separate windows — the dashboard
draws the whole screen itself and hosts the sessions inside it.

![Omni Console — hero](docs/screenshots/hero.png)

<sub>Built with Rust ·
<a href="https://github.com/ratatui/ratatui">ratatui</a> ·
<a href="https://github.com/a-kenji/tui-term">tui-term</a> ·
<a href="https://github.com/doy/vt100-rust">vt100</a> ·
<a href="https://docs.rs/portable-pty">portable-pty</a></sub>

</div>

---

## Why

Plenty of tools manage terminal sessions or Claude instances; none did all of
this the way I wanted:

- **Cross-project.** Watch agents on unrelated repos in one place, not one
  orchestration at a time.
- **Live, not a monitor.** The tile *is* the session — drive Claude from inside
  the dashboard, don't just read its status.
- **Rooms that enforce isolation.** Agents on a shared feature collaborate on a
  private bus; unrelated projects stay mute to each other.

## Features

- 🪟 **Live embedded terminals** — every tile runs a real `claude` in its own
  PTY (parsed by `vt100`, rendered with `tui-term`); type into the focused one.
- 🧩 **Groups** — a *Lead* session plus the agents it spawns, drawn as nested
  tiles in one frame. A group can span multiple repos.
- 🚦 **Status at a glance** — Claude hooks write `working / blocked / idle /
  done` to `state.db`; blocked agents float to the front and light up red.
- 🪫 **Context meter** — each tile header shows the percent of the model's
  context window still free (`NN% ctx`), read from the session transcript and
  coloured green → blue → red as it runs down.
- 📡 **Shared bus + broadcast** — each group gets an isolated [hcom](https://github.com/aannoo/hcom)
  bus so agents talk; one key broadcasts a decision to the whole group.
- 🌱 **Dynamic spawn** — a Lead delegates with `omni spawn`, and the new agent
  appears live as a tile in its group, no restart.
- 🔭 **Glance mode** — collapse every tile to a compact card to keep an eye on
  the whole board.
- 🔎 **Project picker** — a fuzzy finder over the git repos under your home.

## Install

Requires a [Rust toolchain](https://rustup.rs) and [`hcom`](https://github.com/aannoo/hcom)
(`brew install aannoo/hcom/hcom`) for the inter-agent bus.

```sh
git clone https://github.com/BluHal/omniscience
cd omniscience
cargo install --path .     # installs `omni` to ~/.cargo/bin
```

Run it in a real terminal:

```sh
omni
```

## Usage

Open a project with `^n`, pick a repo, and Claude starts live in a tile. Press
`i` to type into it, double-tap `esc` to step back out to navigation.

| Key | Action |
|-----|--------|
| `^n` | **new project** — fuzzy picker over git repos under `~`, opens Claude in a tile |
| `i` / `⏎` | **type** into the focused tile (insert mode) |
| `esc esc` / `^\` | back to **nav** mode without interrupting Claude (a lone `esc` still reaches it) |
| `↹` · arrows | move **focus** between tiles |
| `^b` | **broadcast** a decision to the focused tile's group (type, `⏎` to send) |
| `z` | **glance** mode (compact keep-an-eye cards) |
| `+` / `-` | **resize** the focused tile's column wider / narrower (`=` resets) |
| `!` | **jump** to a blocked agent |
| `m` | **minimize** the focused tile to the dock — its Claude keeps running (click a dock chip or `m` to restore) |
| `x` | **close** the focused tile — ends its Claude |
| `?` | help · `q` quit |

On the welcome screen, use `↑`/`↓` to move between **open a project** and your
recent projects, then `⏎` to open the highlighted one.

### Spawning collaborators

A Lead is launched knowing it can grow its own group at runtime:

```sh
omni spawn <room> <role> [--dir <path>] [brief]
```

Running this from inside a Lead (Claude shells out to it) queues an agent that
the dashboard picks up and opens as a new tile in that group, wired to the same
hcom bus. `--dir` places the agent in **another repo** while keeping it on the
group's shared bus (cross-repo groups). Answer a blocked agent by typing in its
tile; share a decision with everyone via `^b`.

### The message bus

Every tile opens with a first-run prompt to `hcom start`, which joins the
session to its group's [hcom](https://github.com/aannoo/hcom) bus. From then on
the bus hooks fire each turn to deliver messages and status — that's what lets
agents talk and `^b` broadcasts land.

If a session doesn't need the bus — a solo project, or the cross-agent chatter
gets noisy — stop it by hand from inside that tile:

```sh
hcom stop          # disconnect this session; re-join later with hcom start
```

`hcom start` rejoins whenever you want it back.

## Screens

| Hero | Glance | Picker |
|------|--------|--------|
| ![hero](docs/screenshots/hero.png) | ![glance](docs/screenshots/glance.png) | ![picker](docs/screenshots/picker.png) |

## How it works

```
            ┌──────────────────────────────┐
            │  omni — Rust/ratatui          │  bespoke compositor, owns the PTYs
            │  ┌────────┐ ┌────────┬───────┐│
            │  │ lead   │ │ lead   │ agent ││  each tile = a live `claude` PTY
            │  └────────┘ └────────┴───────┘│
            └──────┬───────────────┬────────┘
        CC hooks   │               │   CC hooks + hcom hooks
                   ▼               ▼
   ~/.omni/state.db (status, ALL)   .omni/<room>/.hcom (chat, roomed)
```

Two data layers:

- **`state.db`** (`~/.omni`) — one row per session, updated by omni's own Claude
  hooks (`omni hook <event>`). The dashboard polls it for live status.
- **per-group hcom bus** (`.omni/<room>/.hcom`) — the agent↔agent chat and
  decision broadcasts, surfaced when a room is open.

The visual system follows the Claude Design *"Terminal Dashboard UI System"*
handoff. Design and engineering decisions are recorded as ADRs in
[docs/adr/](docs/adr/); the domain glossary is in [CONTEXT.md](CONTEXT.md).

## Project layout

```
src/          Rust source (compositor, terminals, state, launch)
docs/adr/     architecture decision records
CONTEXT.md    domain glossary
```

## Roadmap

- [x] Live embedded terminals, groups, focus/insert
- [x] Status via hooks → `state.db` (blocked/idle/done)
- [x] hcom bus, broadcast, dynamic `omni spawn`
- [x] Home-wide git-repo project picker (with recents + fuzzy highlight)
- [x] Cross-repo groups (`omni spawn --dir`), glance mode, help, resume-last
- [ ] **Persistence across restarts** — omni currently owns the PTYs
  in-process, so quitting ends the sessions. A detach/daemon split (PTY host +
  TUI client) is the next milestone — see
  [ADR-0003](docs/adr/0003-rust-compositor-embedded-ptys.md).

## Prior art

[Claude Agent Teams](https://code.claude.com/docs/en/agent-teams) ·
[hcom](https://github.com/aannoo/hcom) ·
[Recon](https://agent-wars.com/news/2026-03-14-recon-tmux-tui-claude-code-sessions) ·
[VibeMux](https://github.com/UgOrange/vibemux) ·
[claude-squad](https://github.com/smtg-ai/claude-squad)
