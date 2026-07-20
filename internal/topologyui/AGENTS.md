# internal/topologyui

## Purpose

- Owns the interactive terminal topology UI: rendering, input, commands, menus, actions, shell/VNC launch flows, connect mode, move mode, and runtime refresh wiring.
- Translates labs and runtime state into user-visible terminal behavior.

## Ownership

- `app.go` owns app lifecycle, terminal loop, runtime refresh scheduling/application, service synchronization, and pending external shell/VNC execution. Desired-state reconciliation belongs to `foxlabd`/`internal/workload`.
- `runtime_access.go` owns daemon-first runtime snapshots, direct live-status fallback, serialized runtime reads, terminal-session opening, and runtime connection cleanup.
- `model.go` maps `.lab` topology into graph nodes, edges, details, desired state, and layout positions.
- `render.go` and `inventory*.go` style files own visible terminal output and route rendering.
- `input.go`, `commands.go`, `actions.go`, `menu.go`, `connect.go`, and `move.go` own interaction behavior.
- `shell.go` and `vnc.go` own user-facing shell/VNC command preparation and status text.

## Local Contracts

- Preserve terminal-native behavior: tight spacing, readable inventory/resource ports, border/frame selection instead of highlighted text, minimal solid context menus, and the bottom console/help area.
- Keep `.lab` desired config separate from runtime truth. Runtime status and assigned VNC ports come from `WorkloadStates`/runtime XML discovery, not saved placeholders.
- Surface concrete libvirt/containerd errors in `State.Message` when available.
- Connect and NIC flows must preserve explicit NIC indexes, including direct workload-to-workload links.
- Route rendering and move mode should remain smooth; route caches must be reset when topology/layout/runtime display inputs change.
- User-visible command names, menu labels, and help text are stable UI contracts.

## Work Guidance

- Put command parsing and UI wiring here, but delegate durable mutations to `internal/topology`.
- Treat screenshots and smoke renders as behavioral evidence for visual work.
- Update focused tests for command dispatch, menu actions, rendering, movement, connect flows, shell/VNC launch setup, and runtime status mapping.
- Avoid broad UI restyles unless the task is specifically visual; small spacing changes are often visible regressions.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/topologyui`
- Smoke render for visual changes:
  - `GOCACHE=/tmp/foxlab-cli-go-build go run ./cmd/foxlab --no-raw --width 90 --height 24`
- Full regression when behavior crosses packages:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
