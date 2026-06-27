# Prototype: dashboard-as-hub + in-room agent chat

**Run:** `go run ./proto/dashboard`

**Question:** when you open a room, how should you see & talk to its agents?
Each agent has a chat thread; you read any, type into any. Agents are dynamic
(start any agent at runtime). `tab` toggles the in-room layout — **FOCUS** (one
chat full-screen + tab strip) vs **TILED** (all chats side-by-side).

**Keys:** rooms list — `↑↓` move, `enter` open, `n` new room, `tab` layout, `q` quit.
In a room — `↑↓`/`←→` switch agent, type a message, `enter` send, `^a` add agent,
`tab` toggle, `esc` back. Sending to a **blocked** agent unblocks it (the core loop).

Throwaway; fake data. Keep the model + Update, delete the rendering.

## The load-bearing decision the prototype can't make: what IS "agent chat"?

To both *show* a thread and *type into* it, pick a source + a write channel.

| Source (read) | What you see | Cost |
|---|---|---|
| **hcom messages** | inter-agent bus + your DMs/answers | ~free — already the planned substrate (SPEC dec. 6) |
| CC transcript tail | the agent's real prompts/responses/tool calls | parse jsonl; SPEC dec. picked hooks over transcript-tailing |
| tmux capture-pane | exactly the attached view | ANSI soup, unstructured |

| Write channel | Effect |
|---|---|
| **hcom send** | wakes agent at turn boundary (SPEC dec. 5) — the "answer a blocked agent" flow |
| tmux send-keys | types into the live prompt; fights the turn loop |

**Recommendation:** chat = **hcom** (read) + **hcom send** (write). Keeps the
dashboard a thin viewer over `state.db` + hcom, reuses locked decisions 5/6/10,
adds no terminal emulator or transcript parser. The agent's raw work stays in
tmux; `Enter → attach` is the deep-dive (dec. 10). The dashboard chat is the
*message/decision layer*, not a full terminal mirror.

This contradicts SPEC dec. 3/10 ("disposable status viewer, no emulator") only
if you want the full transcript mirror. If hcom-as-chat is enough, no conflict.

## Dynamic spawn (the other ask)

"Start any agent" + "another Claude spawns agents into a room when prompted"
→ a small CLI an orchestrator can shell out to, e.g. `omni spawn <room> <role>
[brief]`, parallel to `omni up`. The dashboard already shows agents appearing
live (it polls state.db). No layout change — just a new command + the agent row.

## Verdict (2026-06-27)

- **Layout: TILED** — open a room → every agent's chat side-by-side, focus one to type.
- **Chat source: hcom** — read the room's hcom messages, write via `hcom send`.
  No transcript parser, no terminal emulator. Dashboard stays a thin viewer.
- `Enter → tmux attach` stays as the deep-dive for an agent's raw tool output.

Folded into SPEC (decisions 12 & 13). Once the real TILED+hcom view lands in
tui.go, **delete this prototype dir** — the model/Update was the keeper.
