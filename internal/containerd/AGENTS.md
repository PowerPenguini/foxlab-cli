# internal/containerd

## Purpose

- Owns containerd runtime integration for container lifecycle, shell access, rootfs disk preparation, mount cleanup, and host bridge attachment.

## Ownership

- `runtime.go` owns containerd client setup, namespace selection, state discovery, start/stop, image pull, OCI spec options, config hash labels, and network attachment.
- `disk.go` owns qemu-nbd based container disk rootfs preparation and cleanup.
- `shell.go` owns container shell command/session setup.
- `*_test.go` files should cover runtime state mapping, config hashing/recreation decisions, shell setup, disk helper behavior, and cleanup paths.

## Local Contracts

- Default containerd address is `/run/containerd/containerd.sock`; default namespace is `foxlab`.
- Container runtime state is runtime truth and must not be written into `.lab`.
- Desired state is reconciled through `internal/workload`; containerd should implement runtime actions, not policy.
- Disk-backed containers mount the explicitly attached qcow2 path from `internal/lab` and must clean qemu-nbd/mount state on stop.
- Host networking should go through `internal/hostnet` so VM and container interface naming stays consistent.
- Return concrete containerd, image, mount, qemu-nbd, and network errors to callers.

## Work Guidance

- Keep privileged host operations behind small helpers or runners so tests can replace them.
- Preserve idempotence: starting an already running matching container should attach networking and return cleanly; changed config should recreate the container deliberately.
- Avoid hiding missing system tooling behind generic errors; name the missing tool.
- Be careful with mount cleanup because failed cleanup leaves host state outside the repo.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/containerd`
- Full regression for runtime contract changes:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
