# internal/tui/graph

## Purpose

- Owns the minimal graph data model consumed by terminal rendering.

## Ownership

- `model.go` owns `Model`, `Node`, `Edge`, key construction, and node key helpers.

## Local Contracts

- Keep this package free of topology-specific behavior and runtime concepts.
- `Key(type, id)` format is part of the rendering contract; update callers and tests together if it changes.
- Nodes carry display-ready fields, but layout and drawing decisions belong in `internal/topologyui`.

## Work Guidance

- Prefer adding graph-neutral fields only when multiple renderers or UI paths need them.
- Avoid importing lab, topology, virt, or containerd packages here.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/tui/graph`
