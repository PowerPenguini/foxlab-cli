# internal/tui

## Purpose

- Owns low-level terminal canvas primitives used by the topology UI.
- Provides reusable drawing, clipping, line merging, box drawing, styling, and text fitting helpers.

## Ownership

- `canvas.go` owns the terminal canvas, cells, line masks/runes, ANSI string output, boxes, lines, clearing, and fitting helpers.
- `graph/` owns the minimal graph model shared by rendering code.

## Local Contracts

- Drawing helpers must clip safely at canvas bounds.
- Line drawing should merge intersections through line masks instead of overwriting existing routes blindly.
- Keep primitives terminal-native and dependency-light.
- Preserve ANSI reset behavior so styled output does not leak into following terminal content.
- Width/height handling must be deterministic for tests and smoke renders.

## Work Guidance

- Put only generic terminal drawing behavior here; topology-specific layout, labels, menus, and commands belong in `internal/topologyui`.
- Add focused unit tests before changing line rune selection, clipping, string output, or fitting behavior.
- Be conservative with Unicode width assumptions; visual changes should be smoke-rendered.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/tui/...`
- Full visual regression when primitives affect topology rendering:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
  - `GOCACHE=/tmp/foxlab-cli-go-build go run ./cmd/foxlab --no-raw --width 90 --height 24`

## Child DOX Index

- `graph`: minimal graph data structures and stable node/edge key helpers.
