# internal/hostnet

## Purpose

- Owns host bridge and interface helpers used by VM and container runtimes.
- Encapsulates Linux `ip`/`nsenter` style operations behind testable runners.

## Ownership

- `bridge.go` owns command runner plumbing, bridge ensure/attach/detach behavior, tap/veth naming, direct-link bridge lookup, and workload endpoint matching.
- `*_test.go` files should cover command sequences, generated interface names, bridge names, direct network links, missing switch errors, and detach cleanup.

## Local Contracts

- Keep generated interface names within Linux's 15-character limit.
- Use managed names from `internal/lab` for FoxLab bridges, domains, containers, and direct network links.
- VM and container networking should share bridge/link semantics here rather than duplicating host commands in runtime packages.
- Detach paths should be best-effort where appropriate, but attach paths must return concrete command errors.
- Do not persist runtime interface names or host bridge state into `.lab`.

## Work Guidance

- Keep shell execution behind `CommandRunner` so behavior remains testable without root.
- Treat direct network links as first-class endpoints alongside switch-backed NICs.
- Be cautious with command order: failed attach operations can leave host interfaces behind.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/hostnet`
- Full regression for network behavior changes:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
