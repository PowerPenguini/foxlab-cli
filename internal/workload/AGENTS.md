# internal/workload

## Purpose

- Owns runtime-neutral workload reconciliation contracts shared by libvirt, containerd, and the TUI.
- Keeps desired `.lab` state separated from actual runtime state.

## Ownership

- `workload.go` owns workload refs, type names, keys, and the runtime interface.
- `composite.go` owns runtime fan-out across VM and container backends.
- `reconciler.go` owns desired-vs-actual reconciliation policy and result reporting.
- `*_test.go` files should cover key format, composite routing, reconcile decisions, actions, errors, and stopped-state semantics.

## Local Contracts

- Runtime keys must remain stable across packages; UI state maps and backend state maps depend on the same key format.
- The reconciler reads desired state from `.lab` and actual state from `Runtime.States`.
- Empty, missing, shutoff, and stopped actual states are treated as stopped.
- Runtime implementations execute start/stop; this package decides when to call them.
- Preserve useful per-workload errors so UI and tests can identify the failing backend/action.

## Work Guidance

- Keep this package free of libvirt/containerd imports.
- Add tests when changing state names, desired-state handling, or composite runtime dispatch.
- Prefer explicit workload refs over raw strings at package boundaries.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/workload`
- Full regression for reconciliation changes:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
