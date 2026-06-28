# Omniscience

A terminal dashboard to launch, monitor, and **drive** multiple live Claude Code
sessions side-by-side — across the same or different projects — with isolated
groups where agents collaborate. This glossary fixes the language; design
decisions live in `docs/adr/`.

## Language

**Tile**:
A region of the dashboard hosting a live, interactive **embedded terminal** that
runs a real Claude Code **Session**. You see Claude's native TUI and type into it
directly — it is not a distilled/parsed chat view.
_Avoid_: panel, pane (reserve "pane" for the tmux/multiplexer primitive), chat
(the chat is what's *inside* a tile, not the tile itself).

**Session**:
One running `claude` process the user can interact with through a Tile.
_Avoid_: chat, instance, window.

**Project**:
The directory a Session runs in. An *attribute* of a Session, **not** a
container — a Group can span several Projects.

**Group**:
A **Lead** Session plus the agents it spawned, sharing **one hcom bus** and
rendered as nested Tiles (the "big tile holding 3 sub-tiles"). The unit of
collaboration and isolation: distinct Groups never share a bus. A lone Session
is a Group of one. A Group can span multiple Projects (e.g. a frontend agent in
one repo, a backend agent in another). **Replaces the old "Room"** (ADR-0002).
_Avoid_: room, team, session-group.

**Lead**:
The Session at the root of a Group — the one that spawns the other agents (by
shelling out, e.g. `omni spawn`). The user types into the Lead to drive the
whole Group.
_Avoid_: orchestrator, parent, boss.

**Agent**:
A non-Lead Session spawned into a Group by its Lead. (The Lead is also "an
agent" loosely, but "Agent" alone means a spawned member.)

**Dashboard**:
The single surface `omni` renders, where every Group's Tiles are laid out
together. It is not a separate viewer over the Sessions — it *is* the surface
the Sessions live in.
_Avoid_: monitor, overview, TUI.

## Notes

- Grouping is **Lead-rooted only** — two independently launched Sessions never
  auto-merge into a Group, even on the same Project. Collaboration always comes
  from a Lead spawning members.
