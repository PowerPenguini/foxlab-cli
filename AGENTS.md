# FoxLab CLI

## Purpose

- This repository builds the FoxLab terminal topology app and runtime helpers.
- The TUI loads `.lab` files and renders/edits topology state; the background reconciliator converges desired workload state through libvirt and containerd backends.
- User data defaults to `~/.foxlab`; the default lab file is `~/.foxlab/default.lab`.

## Ownership

- `cmd/foxlab` owns TUI CLI flags and app startup.
- `cmd/foxlabd` owns the background reconciliator CLI and systemd-facing daemon loop.
- `internal/lab` owns the `.lab` schema, validation, persistence, and storage paths under `~/.foxlab`.
- `internal/topology` owns model mutations and desired-state operations that update labs.
- `internal/topologyui` owns terminal rendering, input, commands, menus, and user-visible interaction behavior.
- `internal/virt` owns libvirt XML, console, VNC, and VM-facing integration.
- `internal/containerd` owns containerd runtime integration, shell, disk layer mounting, and container lifecycle behavior.
- `internal/workload` owns runtime-neutral workload reconciliation interfaces.
- `internal/hostnet` owns host bridge/network helpers.
- `internal/tui` owns low-level terminal canvas primitives.

## Local Contracts

- Treat `.lab` as the durable user-facing format. Top-level lab identity is `name`, not `id`; keep legacy `id` loading compatible unless explicitly removing migration support.
- Workload, disk, switch, NIC, and link identifiers still use `id` fields inside the lab.
- Keep desired lab configuration separate from runtime truth. Runtime status, assigned VNC ports, and live state belong in runtime state, not saved config placeholders.
- Disks and qcow2 layers are stored under `~/.foxlab/labs/<lab-name>/`; do not silently write managed disks elsewhere.
- Default disk behavior is explicit: attaching a base disk to a VM or container writes to that base, attaching an existing qcow2 layer writes to that layer, and no layer is created unless the user explicitly creates one.
- Installed binary names are `foxlab` for the TUI and `foxlabd` for the reconciliator.
- Preserve terminal-native behavior in the TUI: tight spacing, readable inventory/resource ports, border/frame selection, minimal solid context menus, and a bottom console/help area.
- Do not hide libvirt/containerd errors behind generic UI text when a concrete runtime error can be surfaced.

## Work Guidance

- Before changing code, read the nearest `AGENTS.md` for the package you are touching and follow it together with this root DOX.
- Prefer existing package boundaries over new abstractions. Put schema changes in `internal/lab`, state-changing commands in `internal/topology`, and UI wiring in `internal/topologyui`.
- Keep user-visible command names, menu labels, and `.lab` keys stable once introduced.
- Update tests with behavior changes, especially parser validation, topology mutations, and UI command dispatch.
- Use `rg` for code search and keep generated or binary churn out of reviews unless the task requires it.
- Do not revert unrelated local changes; this repo may have pending user work.
- Use `apply_patch` for manual edits.

## Verification

- Fast full test pass:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
- Build:
  - `make build`
- Terminal smoke render:
  - `make smoke`
  - or `GOCACHE=/tmp/foxlab-cli-go-build go run ./cmd/foxlab --no-raw --width 90 --height 24`
- Install-path check when packaging changes:
  - `make install DESTDIR=/tmp/foxlab-install-check PREFIX=/usr`
  - Expected installed paths: `/tmp/foxlab-install-check/usr/bin/foxlab`, `/tmp/foxlab-install-check/usr/bin/foxlabd`, and `/tmp/foxlab-install-check/usr/share/systemd/user/foxlabd.service`.
- For changes involving default lab creation, test with a temporary `HOME` and preserve the real user `GOPATH`/module cache:
  - `HOME=/tmp/foxlab-check GOPATH=/home/powerpenguini/go GOMODCACHE=/home/powerpenguini/go/pkg/mod GOCACHE=/tmp/foxlab-cli-go-build go run ./cmd/foxlab --no-raw --width 60 --height 16`

## Child DOX Index

- `internal/lab`: `.lab` schema, validation, default storage layout, and file IO.
- `internal/topology`: lab mutation layer for VMs, containers, links, disks, and service state.
- `internal/topologyui`: interactive TUI behavior, rendering, command parsing, menus, actions, shell/VNC/connect flows.
- `internal/virt`: libvirt XML, console, VNC, and VM runtime integration.
- `internal/containerd`: containerd runtime, shell, disk preparation, mount cleanup, and container lifecycle integration.
- `internal/workload`: shared workload runtime contracts and reconciliation.
- `internal/hostnet`: host networking helpers.
- `internal/tui`: low-level canvas/rendering primitives.
- `internal/tui/graph`: minimal graph model consumed by rendering code.
