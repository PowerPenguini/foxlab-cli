# internal/topology

## Purpose

- Owns model mutations and desired-state operations that update loaded labs.
- Provides command-facing behavior for VMs, containers, switches, external links, direct network links, disks, and service state.

## Ownership

- `service.go` owns the mutable `Service`, save-and-refresh flow, lookups, ID helpers, and desired-state setters.
- `mutations.go` owns workload/network creation, configuration, deletion, NIC edits, and link cleanup.
- `disks.go` owns disk create/attach/detach/merge/delete behavior and qemu-img integration.
- `*_test.go` files should cover mutation results, saved lab effects, cleanup of related links/disks, and error text.

## Local Contracts

- Mutations operate on durable desired lab state only. They do not inspect libvirt/containerd live state directly.
- Every successful state change should persist through `SaveAndRefresh()` so normalized/validated lab state is reloaded.
- Removing workloads or NICs must also remove stale direct network links and detach managed disk layers where applicable.
- Disk attachment activates the selected disk only: a base disk is attached directly, an existing layer is attached directly, and new qcow2 layers are created only by explicit layer actions.
- Command result strings are user-visible; keep names and wording stable once introduced.

## Work Guidance

- Put lab-changing command behavior here, then keep `internal/topologyui` as a thin parser/wiring layer.
- Prefer small helpers that preserve existing command grammar over broad abstractions.
- When adding arguments, reject unsupported keys explicitly and update command tests.
- Keep filesystem writes for managed disks under paths returned by `internal/lab`.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/topology`
- Full regression for cross-package behavior:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
