# Omniscience (`omni`)

A lazygit-style terminal dashboard to launch, monitor, and steer parallel
Claude Code agents across projects, with isolated rooms where agents on one
feature collaborate via [hcom](https://github.com/aannoo/hcom).

**Design & rationale:** [SPEC.md](SPEC.md) — read it first.

## Status

First vertical slice (tracer bullet): `omni up <room>` spawns agents in a
detached tmux session wired to status hooks that write `~/.omni/state.db`;
`omni` (no args) is the live dashboard. The chat/hcom layer, answer-broadcast,
and Enter-to-attach are not built yet.

## Prerequisites

Go 1.26+, `tmux`, and the `claude` CLI (authenticated). `hcom` is only needed
for the (not-yet-built) chat layer.

## Build

```
go build -o omni .
```

## Use

A room is a folder of brief markdown files — one agent per file, role = filename:

```
.omni/feature-x/
  frontend.md
  backend.md
```

```
omni up feature-x   # spawn an agent per brief in the detached "omni" tmux session
omni                # live dashboard
tmux attach -t omni # drop into the agents
```
