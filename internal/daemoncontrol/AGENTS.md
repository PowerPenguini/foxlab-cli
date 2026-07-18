# internal/daemoncontrol

## Purpose

- Owns systemd-facing foxlabd status, apply, unit, drop-in, and privileged file-management behavior.
- Keeps operating-system service control outside the interactive topology UI.

## Local Contracts

- Applying an already-active lab is idempotent.
- Switching labs destroys resources owned by the previously configured lab before starting the new daemon configuration.
- Concrete systemd, filesystem, and privilege errors must remain visible to callers.
- Tests must use temporary configuration roots and injected command runners; never touch the live systemd installation.

## Verification

- `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/daemoncontrol`
