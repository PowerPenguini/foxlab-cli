# internal/lab

## Purpose

- Owns the durable `.lab` schema, normalization, validation, file IO, and default storage paths under `~/.foxlab`.
- Keeps compatibility with existing user lab files while making new saved output deterministic.

## Ownership

- `lab.go` owns public schema structs; `io.go` and `normalize.go` own load/save and normalization.
- `validate*.go` owns validation orchestration and resource-specific validation sections; `lookup.go` owns shared schema lookups and endpoint equality.
- `storage.go` owns `~/.foxlab` storage roots and managed disk/layer paths.
- `*_test.go` files should cover schema compatibility, validation errors, path behavior, and save/load round trips.

## Local Contracts

- Top-level lab identity is the `name` YAML/JSON key backed by `Lab.ID`; keep legacy top-level `id` loading compatible unless migration support is explicitly removed.
- Inner workload, disk, switch, NIC, link, and endpoint identifiers remain `id` fields.
- Desired runtime intent is persisted as `desiredState`; actual runtime state, assigned VNC ports, and transient errors do not belong in saved lab config.
- Managed lab data lives under `~/.foxlab/labs/<lab-name>/`; do not silently route managed disks or layers elsewhere.
- Keep default disk behavior layered: base disks are durable inputs, workload attachments should use qcow2 layers.
- Use strict YAML decoding for lab files so unknown keys are surfaced.

## Work Guidance

- Put schema additions, defaulting, normalization, validation, managed naming, and storage path decisions here before wiring UI or runtime behavior.
- Add compatibility tests for any `.lab` key change, especially when new fields can be omitted by older files.
- Keep validation messages concrete enough for the TUI to show directly.

## Verification

- Focused package test:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/lab`
- Full regression when schema changes affect other packages:
  - `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
