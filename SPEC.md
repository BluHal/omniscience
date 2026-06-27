# Omniscience — spec

A terminal dashboard (lazygit-style) to launch, monitor, and steer parallel
Claude Code agents across projects — with isolated rooms where agents on one
feature chat with each other while unrelated projects stay separate.

One all-seeing view over every agent, everywhere. Project: **Omniscience**.
Command/binary: **`omni`**. Personal daily-driver tool, built *exactly my way* —
existing tools cover pieces of this but none fit the whole workflow.

---

## Why build it (the landscape, and the gap)

Plenty exists; none does all of it the way I want:

| Tool | What it does | Why not just use it |
|---|---|---|
| [Claude Code Agent View](https://code.claude.com/docs/en/agent-view) (`claude agents`) — native | One screen for background sessions in *one* orchestration | No cross-*project* unification |
| [Agent Teams](https://code.claude.com/docs/en/agent-teams) — native, experimental | Multi-instance, file-based message bus, in-terminal panel | Lead-driven (a Claude session orchestrates, not an external launcher); can't pre-author membership; one-team-per-session; wants to own tmux; no Ghostty; experimental instability |
| [Recon](https://agent-wars.com/news/2026-03-14-recon-tmux-tui-claude-code-sessions) / [VibeMux](https://github.com/UgOrange/vibemux) | lazygit/k9s-style TUI session monitors | Close, but not *my* layout/workflow |
| [claude-squad](https://github.com/smtg-ai/claude-squad) / Conduit / Agent Deck | Multi-CLI session managers | Generic; not built around room isolation + decision broadcast |
| [hcom](https://github.com/aannoo/hcom) | Hook-based inter-agent message/event bus | Not a dashboard — but the right *substrate* to reuse (see below) |

**The wedge:** one unified cross-project/cross-room dashboard where agents on a
shared feature collaborate and unrelated projects stay isolated. The bus is
reused (hcom); the dashboard is the thing being built.

---

## Locked decisions

| # | Decision | Choice | Rationale |
|---|---|---|---|
| 1 | Why build | Personal daily-driver, my way | Existing tools don't fit the whole flow |
| 2 | Scope | **Spawn-only** — omni launches *all* CC work | Owning the launch makes rooms, hooks, and bus wiring possible |
| 3 | Hosting | **tmux panes + CC hooks** | tmux already solves PTY/persistence/reattach; hooks give structured status. No terminal emulator. |
| 4 | Isolation unit | **Explicit room**, default name = project; "no room" = monitored-but-mute | Only model that *enforces* "must not communicate" while keeping the common case one keystroke |
| 5 | Delivery | **Wake-on-idle** at turn boundaries | Real-time-*enough* without fighting CC's turn loop (hcom also injects between tool calls — acceptable) |
| 6 | Bus | **hcom**, one `HCOM_DIR` per room | Externally driveable, hook-based, doesn't fight pane ownership. Per-room `HCOM_DIR` = isolated SQLite bus (hcom's own documented "per-project isolation"). Broadcast-within-room = "answer all". |
| 7 | Data model | **Two layers** | `~/.omni/state.db` = status of *every* session (your hooks); hcom per-room db = the chat (roomed only) |
| 8 | Launch artifact | **Folder-of-briefs convention** | The brief docs the workflow already produces *are* the spec. No schema to invent. |
| 9 | Stack | **Go + Bubble Tea** | Built for dense lazygit-style TUIs; the closest competitors are Go; I/O-bound so Rust's tax buys nothing |
| 10 | Process model | **Standalone TUI + detached tmux session** | Dashboard is a disposable viewer — its crash/restart must never touch running agents. Reopen re-attaches. |
| 11 | Answer propagation | **Direct-to-agent by default, one key to broadcast as a decision** | The requirement is sharing *decisions*, not every clarification. Auto-broadcast wakes+bills every agent on every keystroke. |

---

## Architecture

```
                    ┌─────────────────────────────┐
                    │  omni TUI (Go + Bubble Tea)  │  standalone, disposable
                    └──────────────┬──────────────┘
            reads state.db │       │ shells out to: tmux, hcom send
            reads room chat │       │ Enter → suspend + tmux attach
                    ┌───────▼───────┴───────────────────┐
                    │  detached tmux session "omni"      │  agents persist here
                    │  pane: fe-agent   pane: be-agent   │
                    └───────┬─────────────────┬──────────┘
              CC hooks      │                 │   CC hooks + hcom hooks
                           ▼                 ▼
        ~/.omni/state.db (status, ALL)    .omni/<room>/.hcom/*.db (chat, roomed)
```

**Two data layers (decision 7):**

- `~/.omni/state.db` (SQLite/WAL, fixed path) — written by *omni's own* hooks
  on every spawned session. One `sessions` row per agent, updated on each event.
  The dashboard's entire cross-project view is one query against this file.
  - `SessionStart` → upsert row, `status=working`
  - `PreToolUse`  → `current_activity = <tool>`, `status=working`
  - `Notification`→ `status=blocked` *(the "needs me now" signal — the reason
    we use hooks instead of tailing transcripts)*
  - `Stop`        → `status=idle`
  - session end   → `status=done`
- hcom per-room db (`.omni/<room>/.hcom/`) — the agent↔agent chat + decision
  broadcasts. Surfaced only when a room is opened.

Minimal `state.db` schema — one table is enough; an `events` log is deferred:

```sql
CREATE TABLE sessions(
  id TEXT PRIMARY KEY, room TEXT, project_path TEXT, role TEXT,
  tmux_pane TEXT, model TEXT, status TEXT, current_activity TEXT,
  started_at INTEGER, last_event_at INTEGER
);
```

---

## The room convention (decision 8)

A room is a folder of brief markdown files. No manifest format.

```
.omni/feature-x/
  frontend.md     ← one agent. role = filename. brief = file contents.
  backend.md      ← one agent.
  .hcom/          ← this room's HCOM_DIR (isolated bus), auto-created
```

`omni up feature-x` →
- one `claude` agent per `.md`, spawned in its own tmux pane
- room name = folder, `HCOM_DIR` = the co-located `.hcom/`
- omni's status hooks + hcom hooks installed; both agents joined to the bus

Per-agent overrides (model, permission mode) go in **optional** YAML frontmatter
in a brief — added only when first needed (YAGNI).

A standalone session launched in **no room** gets status hooks but **no
`HCOM_DIR`** → visible on the dashboard, wired to no bus, can't chat.

---

## Key flows

- **Launch a feature room:** issue → grill → write `frontend.md` + `backend.md`
  into `.omni/feature-x/` → `omni up feature-x`. Two agents, one isolated bus.
- **Cross-project monitoring:** launch standalone sessions through omni (no
  room). All appear in one overview with live status; blocked agents float up.
- **Answer a blocked agent:** dashboard shows FE blocked. Type the answer →
  goes to FE only. If it's a shared decision (token format, API contract), one
  key broadcasts it to the room → all agents wake with it. Broadcasts double as
  a per-room decision log.
- **Drop into an agent:** Enter → suspend Bubble Tea, `tmux attach` into the
  pane, detach → back to dashboard (lazygit's own model).
- **Persistence:** close/crash the dashboard → agents keep running in the
  detached tmux session. Relaunch `omni` → re-reads `state.db`, re-attaches.

---

## Deferred (YAGNI / taste — not forgotten)

- Screen layout + keybindings — resolve with a throwaway prototype, not on paper.
- Room teardown / "done" lifecycle + cleanup.
- Decision-log replay to catch up a restarted or late-joining agent (data's
  already there as tagged broadcasts).
- Per-agent model/permission overrides via brief frontmatter.

## Verification results (2026-06-27) — both spikes PASSED

1. **Isolation — PASS.** `HCOM_DIR=<dir>/.hcom` relocates *all* hcom state: db,
   identities, messages, **and** the Claude hooks (installed into a project-local
   `.claude/settings.json` at the parent of `HCOM_DIR`). Two rooms saw zero
   cross-talk; global `~/.hcom` and `~/.claude/settings.json` stayed untouched.
2. **Injection — PASS.** An external `hcom send` woke an idle agent via the Stop
   hook; it incorporated the message and finished correctly — no turn corruption.
   (hcom delivers by exiting the Stop hook non-zero, so CC cosmetically labels it
   "Stop hook error" — not a real error.)

**Launch-path findings (load-bearing):**
- Launch **plain `claude`** (user's real auth) + `--settings <file-or-json>` to
  inject hooks (no global/project pollution) + env vars for identity. **Do NOT
  use `hcom claude`** — it boots claude under an isolated, *unauthenticated*
  config (theme + login prompts).
- Plain claude + hcom hooks does **not** auto-join the bus; the agent must run
  `hcom start` once (a single pre-approved Bash call) for a name + inbox.
- A fresh project dir triggers claude's one-time folder-trust prompt; spawn into
  already-trusted dirs (the normal case).

## Stack & references

- Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea)
- [hcom](https://github.com/aannoo/hcom) (bus) — MIT, Python, hook-based (`brew install aannoo/hcom/hcom`)
- [CC hooks](https://code.claude.com/docs/en/hooks-guide) (status feed)
- tmux (process hosting / persistence)
