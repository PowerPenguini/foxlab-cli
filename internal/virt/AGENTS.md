# internal/virt

## Purpose

- Owns libvirt integration for VM XML, runtime state, start/stop behavior, console access, VNC port discovery, and VM-facing network attachment.

## Ownership

- `libvirt.go` owns libvirt connection/runtime behavior, VM state discovery, VNC port lookup, domain define/start/stop/undefine, and bridge attach/detach calls.
- `xml.go` owns domain/network XML generation, managed XML checks, disk/image resolution, NAT ranges, direct-link bridges, and VNC XML parsing.
- `console.go` owns VM console session setup.
- `*_test.go` files should cover XML output, VNC parsing, console behavior, and libvirt-facing edge cases that can be tested without a live daemon.

## Local Contracts

- Default libvirt URI is `qemu:///system`.
- Managed domains/networks must be derived from `internal/lab` managed names and include enough metadata to distinguish FoxLab-owned resources.
- Stop should clean up stale libvirt state by destroying when needed, detaching VM NIC host resources, and undefining managed domains.
- Start should redefine inactive managed domains when config changed rather than leaving stale XML.
- Assigned VNC ports are runtime truth discovered from libvirt XML; do not persist auto-assigned ports into `.lab`.
- Return concrete libvirt/XML/network errors so the UI can display them directly.

## Work Guidance

- Keep libvirt XML construction here; do not duplicate XML fragments in UI or topology packages.
- Use `internal/hostnet` for Linux bridge/tap management instead of shelling out from unrelated packages.
- Preserve idempotent start/stop semantics where possible; repeated reconcile steps should not damage a healthy running VM.
- Add tests around generated XML whenever lab schema or VM network behavior changes.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/virt`
- Full regression for runtime contract changes:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
