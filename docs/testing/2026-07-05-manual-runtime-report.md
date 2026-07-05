# FoxLab Manual Runtime Test Report - 2026-07-05

## Scope

This report covers the current `foxlab-cli` worktree after commit `7988c07`
plus the local runtime hardening, UUID/name migration, and test-suite fixes
listed below.

Test host:

- Lab: `/home/powerpenguini/.foxlab/default.lab`
- Daemon: `foxlabd` active
- Container runtime: containerd namespace `foxlab`
- VM runtime: libvirt `qemu:///system`
- OpenVPN: host tunnel `tun0` from `/home/powerpenguini/Downloads/machines_eu-3.ovpn`

Current lab resources exercised:

- VM `Victim-a`, id `d036228c-cd30-56e4-8534-2dabb1ee75e7`, desired `running`, VNC enabled
- VM `Victim-2`, id `5f99f862-a9c0-571e-8368-4a0f3a25e586`, desired `stopped`
- Container `Kali`, id `323eeeef-8ab2-59a9-97f1-96cfc3ceda53`, desired `running`
- Switch `sw3`, mode `nat`, uplinks `OVPN/tun0` and `Internet/wlp0s20f3`
- Container disk `/home/powerpenguini/.foxlab/labs/default/disks/kali.qcow2`

## Summary

| Area | Result | Evidence |
| --- | --- | --- |
| Build | Pass | `make build` |
| Terminal smoke render | Pass | `make smoke` and `/usr/local/bin/foxlab --no-raw --width 100 --height 28` |
| Focused regression tests | Pass | `go test ./internal/virt ./internal/topologyui -count=1` and prior focused runtime/schema packages |
| Full regression suite | Pass | `GOCACHE=/tmp/foxlab-cli-go-build go test ./...` |
| Install path check | Pass | `make install DESTDIR=/tmp/foxlab-install-check PREFIX=/usr` installed expected binaries and systemd unit |
| TUI command/menu flows | Pass | focused `internal/topologyui` command, context menu, connect, disk menu, top ribbon, shell, VNC tests |
| Topology mutation flows | Pass | focused `internal/topology` disk, NIC, link, switch, external, service mutation tests |
| foxlabd CLI/status socket | Fixed / pass | `--once`, empty `--destroy`, and `/tmp` status socket checks pass without chmoding existing socket directories |
| Container shell | Fixed / pass | `foxlab sh Kali` no longer drops at 15s and no longer leaks `foxlab-shell-*` execs after PTY termination |
| VM shell/console | Pass | `foxlab sh Victim-a` opened libvirt serial console `/dev/pts/0` and surfaced the ttyS0/getty hint; VM stayed running |
| Container network reconcile | Fixed / pass | running Kali kept task PID `1880079`; after idempotent attach fix no recurring `vfoxlabd84689b0` delete/recreate logs after `19:33:34` |
| Container disk NBD lifecycle | Fixed / pass | dead `/dev/nbd0` backing caused Kali EIO; `qemu-nbd` now runs in `foxlab-nbd-*.scope`, disk health is checked during reconcile, and Kali recovered with `ls /` OK |
| Daemon binary freshness | Fixed operationally | `foxlabd` had been running from `/usr/local/bin/foxlabd (deleted)`; installed current `./bin/foxlabd`, restarted service, and verified shell does not restart Kali |
| Disk menu layer actions | Fixed / pass | active layer detection now falls back to the workload disk path, and `internal/topologyui` regression tests pass |
| Containerd permission diagnostics | Fixed / pass | no-sudo `sh` and `cp` errors now suggest `sudo` or containerd socket access |
| Container file copy | Pass | host -> Kali -> host SHA256 round trip |
| VM file copy | Blocked by guest setup / clear error | `Victim-a` has no QEMU guest agent; direct `cp` exits quickly with install/restart guidance |
| Container NAT via OpenVPN | Pass | namespace ping to `10.10.14.1` through `tun0`, 0% loss |
| Daemon status socket | Pass | `status` query returns lab path, states, and VNC port |
| VM VNC | Pass | libvirt reports `127.0.0.1:0`; FoxLab launches viewer target `127.0.0.1::5900`; TigerVNC connected |
| First-run default lab creation | Pass | isolated `HOME=/tmp/foxlab-manual-home` created `.foxlab/default.lab` |
| Isolated schema/CLI/protocol checks | Pass | legacy `id:` lab renders, invalid YAML/schema cases fail clearly, status socket rejects unsupported commands structurally |
| Negative CLI validation | Pass | `--no-raw sh`, invalid `cp`, missing source copy, missing lab path, unknown shell target, and `vnc Kali` return useful errors |

## Fix Applied During Testing

### Running daemon was still the old deleted binary

Observed behavior:

- `/proc/<foxlabd-pid>/exe` pointed at `/usr/local/bin/foxlabd (deleted)`.
- `/usr/local/bin/foxlabd` on disk was older than the latest local `./bin/foxlabd`.
- `foxlabd` logs from `17:44-17:45` showed repeated start attempts:

```text
start container:323eeeef-8ab2-59a9-97f1-96cfc3ceda53: rootfs absolute path is required
reconcile failed: start container:323eeeef-8ab2-59a9-97f1-96cfc3ceda53: rootfs absolute path is required
started container:323eeeef-8ab2-59a9-97f1-96cfc3ceda53
```

Interpretation:

- This was a reconciler retry loop while the container rootfs mount was not yet available, not evidence that `foxlab sh Kali` itself restarts the running container.
- The live daemon was also not the current daemon binary after local rebuild/install work, so CLI and daemon behavior could drift.

Operational fix:

```sh
sudo install -m 0755 ./bin/foxlabd /usr/local/bin/foxlabd
sudo systemctl restart foxlabd
```

Verification after restart:

- `systemctl status foxlabd` showed a fresh daemon PID using `/usr/local/bin/foxlabd`, not `(deleted)`.
- container task `foxlab-default-323eeeef-8ab2-59a9-97f1-96cfc3ceda53` stayed PID `1880079` across daemon restart.
- short shell test and long shell test with `sleep 22` both kept the same container PID `1880079`.
- `journalctl -u foxlabd --since ...` had no reconcile/start entries during those shell tests.

### Container shell timed out after about 15 seconds

Observed behavior:

```text
rpc error: code = DeadlineExceeded desc = stream terminated by RST_STREAM with error code: CANCEL
```

The container was not restarting:

- containerd task PID stayed `1880079`
- container `CreatedAt`, `UpdatedAt`, and `foxlab.config.sha256` stayed unchanged
- `foxlabd` logged no reconcile events during the failure

Root cause:

- `internal/containerd/shell.go` created `setupCtx` with a 15 second timeout.
- The same timeout context was passed to `process.Wait(setupCtx)`.
- When setup timeout expired, the Wait stream was canceled even though the shell was still alive.

Fix:

- Keep `setupCtx` for setup operations.
- Use `runCtx` for `process.Wait(...)`, so the interactive session lives until user exit or caller cancellation.
- Pass the containerd namespace into shell/copy cleanup helpers.
- On caller cancellation, force-delete the exec process through `Process.Delete(..., containerd.WithProcessKill)`.
- Wrap direct container shell runs in a signal-aware context for `SIGINT`, `SIGTERM`, and `SIGHUP`.

Verification:

- Before fix: `sudo timeout 40s /usr/local/bin/foxlab sh Kali` failed after about 15s with `RST_STREAM`.
- After fix: `sudo timeout 25s /usr/local/bin/foxlab sh Kali` stayed connected for 25s without `RST_STREAM`.
- After cleanup fix: `timeout 8s script -qfec 'sudo /usr/local/bin/foxlab sh Kali' ...` left `0` `foxlab-shell-*` execs.
- `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/containerd -run 'TestShell|TestContainerShell' -count=1` passed.

### Disk menu active layer could drift from attachment metadata

Observed behavior:

- The disk menu test could switch from `data-layer-2` back to `data-layer`, then attempt to delete `data-layer-2`.
- The UI rebuilt menu actions from current lab state but active-layer detection relied mainly on `Disk.AttachedTo`.
- When the workload disk path and attachment metadata drifted, the active layer could disappear from the menu and the X action targeted detach instead of delete.

Fix:

- `internal/topologyui/disk_actions.go` now falls back to the resolved workload disk path when detecting the attached disk or attached layer.
- `layerDisksForMenu` keeps the current workload disk visible even when attachment metadata is stale or legacy-keyed.

Verification:

- `GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/topologyui -count=1` passed.

### Containerd permission errors were too terse

Observed behavior:

- Running container-backed direct actions without enough socket permission failed with a raw containerd dial error.
- The error was technically correct, but it did not tell the user how to recover.

Fix:

- `internal/containerd.WithAccessHint` adds a targeted hint only when an error contains both `containerd` and `permission denied`.
- `foxlab sh` and `foxlab cp` wrap containerd-backed status, shell, and transfer errors with that helper.

Verification:

```sh
./bin/foxlab sh Kali
./bin/foxlab cp /tmp/does-not-exist Kali:/root/nope
```

Both now include:

```text
run with sudo or grant access to the containerd socket
```

Focused regression:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./cmd/foxlab ./internal/containerd ./internal/topologyui -count=1
```

Result: pass.

### Container networking was reattached on every reconcile tick

Observed behavior:

- `sudo ctr -n foxlab tasks ls` showed Kali still running as task PID `1880079`.
- `journalctl -k` showed `vfoxlabd84689b0` being removed and recreated on bridge `flfoxlabd6814a3` about once per second.
- The log sequence repeated `entered disabled state`, `entered forwarding state`, and `eth0: renamed from pfoxlabd84689b0`.

Root cause:

- `internal/containerd.Runtime.Start` intentionally calls `Bridge.AttachContainer` for a running matching container so lost networking can be restored.
- `internal/hostnet.AttachContainer` was not idempotent: it always deleted `ethN` in the container namespace and the host veth before recreating it.
- Since `foxlabd` reconciles repeatedly, every tick caused a visible network flap even though the container task itself was not restarted.

Fix:

- `AttachContainer` now checks whether `ethN` already exists in the container namespace.
- If it exists, FoxLab leaves the veth in place and only reapplies idempotent link/address/default-route configuration.
- New regression test: `TestAttachContainerDoesNotRecreateExistingContainerNIC`.

Live verification:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/containerd ./internal/workload ./internal/hostnet
GOCACHE=/tmp/foxlab-cli-go-build make install
sudo systemctl restart foxlabd
sudo journalctl -k --since '2026-07-05 19:33:34' --no-pager | rg -i 'vfoxlabd84689b0|flfoxlabd6814a3|eth0: renamed|entered disabled state|entered forwarding state'
sudo ctr -n foxlab tasks ls
sudo nsenter -t 1880079 -n ip -brief addr show eth0
sudo nsenter -t 1880079 -n ip route show default
```

Result:

```text
TASK                                                   PID        STATUS
foxlab-default-323eeeef-8ab2-59a9-97f1-96cfc3ceda53    1880079    RUNNING
eth0@if118732    UP             172.31.104.32/24 fe80::341d:8ff:fe76:e733/64
default via 172.31.104.1 dev eth0
```

No matching kernel flap logs appeared after `19:33:34`.

### Container disk NBD backend could be killed with foxlabd

Observed behavior:

- Kali stayed `running` in containerd, but `ls` inside the container returned `Input/output error`.
- The host kernel logged real block-device errors:

```text
I/O error, dev nbd0, sector ...
EXT4-fs warning (device nbd0): ... comm ls: error -5 reading directory block
```

Root cause:

- The container writable rootfs is an ext4 filesystem from `kali.qcow2` exposed through `/dev/nbd0`.
- `qemu-nbd` was originally started as a child of `foxlabd`, so restarting/stopping the daemon could kill the NBD backend while containerd and the mounted overlay kept running.
- `Runtime.Start` treated an existing matching mount as healthy if the marker matched and a write probe succeeded; it did not verify that the underlying NBD source was still readable.

Fix:

- New NBD connections are launched through `systemd-run --scope --collect --unit foxlab-nbd-<device> qemu-nbd --fork ...`, so the long-lived `qemu-nbd` process is not in `foxlabd.service`'s cgroup.
- `prepareContainerDiskMount` waits until the NBD device is readable before mounting it.
- Reconcile checks mounted container disks for source readability. If the mounted source is unhealthy, the running container is recreated and the stale mount is cleaned instead of being left as a broken rootfs.
- Regression test: `TestPrepareContainerDiskMountReplacesMountWhenSourceUnhealthy`.

Live verification:

```sh
sudo systemctl restart foxlabd
ps -eo pid,ppid,stat,unit,cmd | rg 'qemu-nbd|foxlab-nbd|foxlabd|containerd-shim'
sudo ctr -n foxlab tasks exec --exec-id foxlab-restart-probe-... foxlab-default-323eeeef-8ab2-59a9-97f1-96cfc3ceda53 /bin/sh -lc 'ls / >/dev/null && echo OK'
sudo journalctl -k --since '2026-07-05 22:14:00' --no-pager | rg -i 'nbd0|I/O error|EXT4-fs warning|EXT4-fs error|Buffer I/O'
```

Result:

```text
foxlab-nbd-nbd0.scope /usr/bin/qemu-nbd --fork --connect=/dev/nbd0 /home/powerpenguini/.foxlab/labs/default/disks/kali.qcow2
foxlabd.service       /usr/local/bin/foxlabd ...
TASK                                                   PID        STATUS
foxlab-default-323eeeef-8ab2-59a9-97f1-96cfc3ceda53    2363228    RUNNING
OK
```

No new NBD/EXT4 I/O errors appeared after the daemon restart.

### Status socket setup tried to chmod existing directories

Observed behavior:

- Starting `foxlabd` with `--status-socket /tmp/<name>` failed with:

```text
status socket: chmod /tmp: operation not permitted
```

Root cause:

- `internal/daemonstatus.prepareSocketDir` always ran `chmod 0755` on the socket parent directory, even when the directory already existed and was not owned by FoxLab.

Fix:

- `prepareSocketDir` now only applies ownership/mode changes when it creates the directory itself.
- Existing directories such as `/tmp` are left unchanged.

Verification:

```sh
tmp_lab=$(mktemp)
printf 'name: daemon-status-empty\n' > "$tmp_lab"
sock=$(mktemp -u)
timeout 4s ./bin/foxlabd --lab "$tmp_lab" --status-socket "$sock" --interval 10s
printf 'status\n' | socat - UNIX-CONNECT:"$sock"
```

Result:

```yaml
labPath: /tmp/tmp.LGpLSwLQJe
labName: daemon-status-empty
updatedAt: 2026-07-05T19:27:49.89246123+02:00
```

Regression:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/daemonstatus -count=1
```

Result: pass.

## Automated Test Evidence

### Passing targeted tests

```text
ok foxlab-cli/cmd/foxlab
ok foxlab-cli/internal/workload
ok foxlab-cli/internal/containerd
ok foxlab-cli/internal/virt
ok foxlab-cli/internal/hostnet
```

Command:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./cmd/foxlab ./internal/workload ./internal/containerd ./internal/virt ./internal/hostnet -count=1
```

Additional focused schema/topology/reconciler checks:

```text
ok foxlab-cli/internal/lab
ok foxlab-cli/internal/topology
ok foxlab-cli/internal/reconciler
```

Command:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/lab ./internal/topology ./internal/reconciler ./internal/containerd -count=1
```

### Full suite

Command:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./...
```

Result: pass.

Package output:

```text
ok foxlab-cli/cmd/foxlab
ok foxlab-cli/cmd/foxlabd
ok foxlab-cli/internal/containerd
ok foxlab-cli/internal/daemonstatus
ok foxlab-cli/internal/foxruntime
ok foxlab-cli/internal/hostnet
ok foxlab-cli/internal/lab
ok foxlab-cli/internal/macnat
ok foxlab-cli/internal/reconciler
ok foxlab-cli/internal/topology
ok foxlab-cli/internal/topologyui
ok foxlab-cli/internal/virt
ok foxlab-cli/internal/workload
```

Additional validation:

```sh
make build
make smoke
git diff --check
```

Result: all pass.

### Install and CLI path checks

Command:

```sh
make install DESTDIR=/tmp/foxlab-install-check PREFIX=/usr
```

Result:

```text
/tmp/foxlab-install-check/usr/bin/foxlab
/tmp/foxlab-install-check/usr/bin/foxlabd
/tmp/foxlab-install-check/usr/lib/systemd/system/foxlabd.service
```

Modes:

```text
-rwxr-xr-x /tmp/foxlab-install-check/usr/bin/foxlab
-rwxr-xr-x /tmp/foxlab-install-check/usr/bin/foxlabd
-rw-r--r-- /tmp/foxlab-install-check/usr/lib/systemd/system/foxlabd.service
```

Command:

```sh
./bin/foxlab --help
./bin/foxlabd --help
```

Result:

- `foxlab` usage lists `--lab`, `--no-raw`, dimensions, libvirt URI, containerd socket, and direct `sh`/`vnc`/`cp` actions.
- `foxlabd` usage lists `--lab`, `--interval`, `--once`, `--destroy`, `--status-socket`, libvirt URI, and containerd socket.

Command:

```sh
HOME="$tmp_home" ./bin/foxlab --lab "$lab_path" --no-raw --width 50 --height 12
HOME="$tmp_home" ./bin/foxlab --no-raw --width 50 --height 12 "$lab_path"
HOME="$tmp_home" ./bin/foxlab --lab "$lab_path" --no-raw "$lab_path"
```

Result:

- `--lab` and positional lab path both render successfully.
- combining `--lab` with a positional lab returns `unexpected argument "..."; --lab is already set`.

Command:

```sh
./bin/foxlabd --lab "$empty_lab" --once
./bin/foxlabd --lab "$empty_lab" --destroy
```

Result: both exit successfully for an empty isolated lab.

### Additional isolated schema, CLI, and status protocol checks

These checks used a temporary directory and did not mutate the real default lab.

Legacy lab compatibility:

```sh
./bin/foxlab --lab "$legacy_lab" --no-raw --width 90 --height 20
```

Fixture used top-level legacy `id: legacy-lab` plus non-UUID node IDs
`victim`, `kali`, `lan`, and `wan`. Result:

```text
lab legacy-lab | VM victim | mode:graph
│[VM] victim   │
```

Interpretation:

- legacy top-level `id` still loads as lab identity
- legacy non-UUID node IDs are migrated in memory
- visible names survive migration in the rendered graph

Validation failures:

```sh
./bin/foxlab --lab "$bad_lab" --no-raw
./bin/foxlab --lab "$multi_doc_lab" --no-raw
```

Results:

```text
switch "11111111-1111-4111-8111-111111111111" name is required; switch "11111111-1111-4111-8111-111111111111" uses unsupported mode "impossible"; supported modes are bridge, nat and macnat-bridge
lab file ".../multi.lab" contains multiple YAML documents
```

Duplicate visible-name guard:

```sh
./bin/foxlab --lab "$ambiguous_lab" sh dup
./bin/foxlab --lab "$ambiguous_lab" vnc samevm
```

Result:

```text
duplicate node name "samevm" used by vm 22222222-2222-4222-8222-222222222222 and vm "33333333-3333-4333-8333-333333333333"; duplicate node name "dup" used by vm 11111111-1111-4111-8111-111111111111 and container "44444444-4444-4444-8444-444444444444"
```

Interpretation:

- direct `sh`/`vnc` name ambiguity is blocked at lab validation time
- errors identify the conflicting visible names and stable IDs

Lab path parser conflicts:

```sh
./bin/foxlabd --once --lab "$empty_lab" "$empty_lab"
./bin/foxlab --lab "$empty_lab" --no-raw extra
```

Results:

```text
unexpected argument ".../empty.lab"; --lab is already set
unexpected argument "extra"; --lab is already set
```

Status protocol unsupported command:

```sh
timeout 8s ./bin/foxlabd --lab "$empty_lab" --status-socket "$sock" --interval 30s &
printf 'bogus\n' | socat - UNIX-CONNECT:"$sock"
printf 'status\n' | socat - UNIX-CONNECT:"$sock"
```

Result:

```yaml
errors:
  - unsupported command "bogus"
```

Status still returned structured YAML:

```yaml
labPath: /tmp/.../empty.lab
labName: empty-status
updatedAt: 2026-07-05T19:36:22.922124214+02:00
```

### Focused high-level interaction coverage

Command:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/topologyui \
  -run 'TestCommand|TestContextMenu|TestConnect|TestDiskMenu|TestMouseClickTop|TestGlobalCreate|TestRunStop|TestVNC|TestShell' \
  -count=1 -v
```

Result: pass.

Covered behavior:

- context menu grouping, inline edits, checkbox toggles, move/cancel, and click-outside cleanup
- disk menu attach/detach/delete, active layer delete with X, add disk, add layer, layer variant switching, and custom layer names
- top ribbon Add dropdown and Exit click handling
- command parser create/set/delete for VMs, containers, switches, external links, NICs, direct links, quoted args, duplicate args, and invalid endpoint usage
- desired-state run/stop actions without direct runtime mutation
- shell/console and VNC command setup paths
- connect mode for VM/container NICs, switch-uplink, uplink-switch, direct workload links, target NIC selection, and target NIC creation

Command:

```sh
GOCACHE=/tmp/foxlab-cli-go-build go test ./internal/topology \
  -run 'Test.*Disk|Test.*NIC|Test.*Link|Test.*Switch|Test.*External|Test.*Create|Test.*Delete|Test.*Desired|Test.*Service' \
  -count=1 -v
```

Result: pass.

Covered behavior:

- disk create/attach/detach/delete, rollback on save failure, explicit base/layer behavior, merge, running-workload guard, and container root/data disk behavior
- VM/container NIC add/connect/delete, direct-link reindexing, MAC validation, and disconnect validation
- switch and external link create/set/delete, uplink append behavior, unsupported arg rejection, and cross-type duplicate visible-name rejection
- desired state persistence for VM/container run/stop
- service mutation save-and-refresh behavior, rollback for new-path save failures, and no-op empty set handling

## Manual Runtime Tests

### TUI render

Command:

```sh
/usr/local/bin/foxlab --no-raw --width 100 --height 28
```

Result:

- rendered `default` lab successfully
- visible nodes: `Internet`, `OVPN`, `sw3`, `Victim-a`, `Victim-2`, `Kali`
- no panic or layout corruption observed in non-raw frame

### First-run default lab creation

Command:

```sh
HOME=/tmp/foxlab-manual-home /usr/local/bin/foxlab --no-raw --width 80 --height 20
```

Result:

- created `/tmp/foxlab-manual-home/.foxlab/default.lab`
- file contents:

```yaml
name: default
```

- rendered an empty default lab frame successfully

Additional isolated render check:

```sh
tmp_home=$(mktemp -d)
HOME="$tmp_home" ./bin/foxlab --no-raw --width 40 --height 10
```

Result:

- created `$tmp_home/.foxlab/default.lab`
- default lab contained only `name: default`
- rendered a narrow frame with the top ribbon (`Apply lab`, `Add`, `Exit`) and no panic or text overlap observed in the captured output

### Daemon/runtime status

Commands:

```sh
systemctl is-active foxlabd
printf 'status\n' | socat - UNIX-CONNECT:/home/powerpenguini/.foxlab/run/foxlabd.sock
```

Result:

```yaml
labPath: /home/powerpenguini/.foxlab/default.lab
labName: default
states:
    container:323eeeef-8ab2-59a9-97f1-96cfc3ceda53: running
    vm:5f99f862-a9c0-571e-8368-4a0f3a25e586: missing
    vm:d036228c-cd30-56e4-8534-2dabb1ee75e7: running
vncPorts:
    vm:d036228c-cd30-56e4-8534-2dabb1ee75e7: 5900
```

### Container shell

Commands:

```sh
sudo timeout 25s /usr/local/bin/foxlab sh Kali
sudo ctr --namespace foxlab tasks list
sudo journalctl -u foxlabd --since '3 minutes ago' --no-pager
```

Result:

- shell connected and remained open beyond the old 15s failure point
- container task PID stayed stable at `1880079`
- daemon produced no restart/reconcile log entries during the shell test
- after reinstalling and restarting `foxlabd`, a long `script`-backed shell test running `sleep 22` still kept PID `1880079` and produced no daemon log entries
- after killing a test shell client with `SIGTERM`, the containerd task returned to only the main container process and its `sleep infinity` child
- after `timeout 8s script -qfec 'sudo /usr/local/bin/foxlab sh Kali' ...`, `ctr tasks ps` still showed no `foxlab-shell-*` execs

### VM shell / serial console

Commands:

```sh
timeout 6s script -qfec 'sudo /usr/local/bin/foxlab sh Victim-a' /tmp/foxlab-vm-console-probe.log
sudo virsh --connect qemu:///system domstate foxlab-default-d036228c-cd30-56e4-8534-2dabb1ee75e7
sudo journalctl -u foxlabd --since '1 minute ago' --no-pager
```

Result:

```text
connected to vm console /dev/pts/0; Ctrl-] exits
VM console is a serial port; use VNC unless the guest has ttyS0/getty enabled.
running
-- No entries --
```

Interpretation:

- direct `foxlab sh Victim-a` resolves the visible VM name and opens the libvirt serial console
- the app gives the expected guidance when the guest has no active serial login
- opening the VM console did not trigger daemon reconcile/restart activity

### Container copy host to guest and guest to host

Commands:

```sh
printf 'foxlab manual copy test ...\n' > /tmp/foxlab-manual-copy-src.txt
sudo /usr/local/bin/foxlab cp /tmp/foxlab-manual-copy-src.txt Kali:/root/foxlab-manual-copy-src.txt
sudo /usr/local/bin/foxlab cp Kali:/root/foxlab-manual-copy-src.txt /tmp/foxlab-manual-copy-back.txt
sha256sum /tmp/foxlab-manual-copy-src.txt /tmp/foxlab-manual-copy-back.txt
```

Result:

```text
dadb3a84b1eac2636ff6cde4f1cb40fd8cb955ac7d2bf12293c9fb775c4bae75  /tmp/foxlab-manual-copy-src.txt
dadb3a84b1eac2636ff6cde4f1cb40fd8cb955ac7d2bf12293c9fb775c4bae75  /tmp/foxlab-manual-copy-back.txt
```

The file was visible inside Kali:

```text
-rw-r--r-- 1 root root 50 /root/foxlab-manual-copy-src.txt
foxlab manual copy test 2026-07-05T18:44:23+02:00
```

### VM copy through QEMU guest agent

Commands:

```sh
sudo virsh --connect qemu:///system qemu-agent-command foxlab-default-d036228c-cd30-56e4-8534-2dabb1ee75e7 '{"execute":"guest-ping"}'
sudo timeout 15s ./bin/foxlab cp /tmp/foxlab-vm-copy-probe.txt Victim-a:/tmp/foxlab-vm-copy-probe.txt
```

Result:

- VM `Victim-a` is running.
- libvirt reports that the QEMU guest agent is not configured.
- FoxLab returns a fast, actionable error instead of hanging:

```text
put vm file "d036228c-cd30-56e4-8534-2dabb1ee75e7": vm guest agent unavailable; install qemu-guest-agent and restart the VM: virError(Code=74, Domain=10, Message='argument unsupported: QEMU guest agent is not configured')
```

Interpretation:

- VM copy support is wired through the QEMU guest agent.
- The current live VM cannot complete positive VM copy until the guest has `qemu-guest-agent` installed and the VM is restarted with the guest-agent channel active.

### Container NAT and OpenVPN reachability

Commands:

```sh
pid=$(sudo ctr --namespace foxlab tasks list | awk '/foxlab-default-323eeeef/{print $2}')
sudo nsenter -t "$pid" -n ip -brief addr
sudo nsenter -t "$pid" -n ip route
sudo nsenter -t "$pid" -n ping -c 2 -W 2 172.31.104.1
sudo nsenter -t "$pid" -n ping -c 2 -W 2 10.10.14.1
```

Result:

```text
eth0@if113100 UP 172.31.104.32/24
default via 172.31.104.1 dev eth0
172.31.104.0/24 dev eth0 proto kernel scope link src 172.31.104.32
```

Gateway ping:

```text
2 packets transmitted, 2 received, 0% packet loss
```

OpenVPN gateway ping:

```text
PING 10.10.14.1
2 packets transmitted, 2 received, 0% packet loss
rtt min/avg/max/mdev = 39.351/41.795/44.240/2.444 ms
```

### VM and VNC

Commands:

```sh
virsh --connect qemu:///system domstate foxlab-default-d036228c-cd30-56e4-8534-2dabb1ee75e7
virsh --connect qemu:///system vncdisplay foxlab-default-d036228c-cd30-56e4-8534-2dabb1ee75e7
sudo env PATH=/tmp/foxlab-fake-vnc-bin:$PATH /usr/local/bin/foxlab vnc Victim-a
```

Result:

```text
running
127.0.0.1:0
```

FoxLab viewer target with fake `vncviewer`:

```text
/tmp/foxlab-fake-vnc-bin/vncviewer 127.0.0.1::5900
```

Real TigerVNC also connected to `127.0.0.1:5900` during testing.

### Negative CLI behavior

| Command | Exit | Result |
| --- | ---: | --- |
| `/usr/local/bin/foxlab --no-raw sh Kali` | 2 | `--no-raw cannot be combined with sh, vnc, or cp actions` |
| `sudo /usr/local/bin/foxlab cp /tmp/does-not-exist Kali:/root/nope` | 1 | `open /tmp/does-not-exist: no such file or directory` |
| `/usr/local/bin/foxlab vnc Kali` | 1 | `vm not found: Kali` |
| `./bin/foxlab cp Kali:/tmp/a Kali:/tmp/b` | 1 | `usage: foxlab cp SRC DST; exactly one side must be NAME:/absolute/path` |
| `./bin/foxlab cp /tmp/does-not-exist Kali:/root/nope` without sudo | 1 | `run with sudo or grant access to the containerd socket` |
| `./bin/foxlab sh Kali` without sudo | 1 | `run with sudo or grant access to the containerd socket` |
| `./bin/foxlab sh NoSuchWorkload` | 1 | `workload not found: NoSuchWorkload` |
| `./bin/foxlab vnc NoSuchVM` | 1 | `vm not found: NoSuchVM` |
| `./bin/foxlab --lab /tmp/does-not-exist.lab --no-raw` | 1 | `open /tmp/does-not-exist.lab: no such file or directory` |

## Open Issues Found

### 1. Interactive shell exec cleanup needed namespace-aware delete

Severity: fixed in current worktree.

The app supports `Ctrl-]` to exit, but interrupted PTY sessions previously left `/bin/sh -i` exec processes in the container task. This was not a container restart. The base container PID stayed stable, while `ctr tasks ps` accumulated `exec_id:"foxlab-shell-..."` entries.

Fix:

- cleanup helpers now use the containerd namespace and fall back to `containerd.WithProcessKill`
- `runContainerShell` now passes a signal-aware context into `ExecShell`

### 2. Disk-backed Kali rootfs had prior I/O errors

Severity: medium.

Earlier tests observed `/bin/bash` failing with an `Input/output error` on `libtinfo.so.6`. Current `/bin/sh`, file copy, routing, and shell attach work, but this disk image should remain under observation.

Recommended next step:

- run filesystem checks on the qcow2-backed rootfs during a controlled stop window
- consider adding a runtime health check that reports guest rootfs read failures distinctly

## Files Changed In Current Worktree

```text
M internal/containerd/shell.go
M internal/containerd/file_transfer.go
M internal/containerd/disk.go
M internal/containerd/disk_test.go
M internal/containerd/errors.go
M internal/containerd/errors_test.go
M internal/daemonstatus/status.go
M internal/daemonstatus/status_test.go
M internal/hostnet/attach.go
M internal/hostnet/bridge_test.go
M internal/lab/normalize.go
M internal/lab/validate.go
M internal/lab/lab_test.go
M internal/reconciler/runner_test.go
M internal/topology/*
M internal/topologyui/app.go
M internal/topologyui/app_test.go
M internal/topologyui/disk_actions.go
M internal/topologyui/model.go
M internal/topologyui/shell.go
M internal/virt/xml_test.go
A docs/testing/2026-07-05-manual-runtime-report.md
```

`internal/containerd/shell.go` contains the fix that prevents interactive container shells from being canceled after the 15 second setup timeout and makes shell exec cleanup namespace-aware.

`internal/containerd/file_transfer.go` uses the same namespace-aware forced exec cleanup on canceled copy operations.

`internal/containerd/disk.go` runs `qemu-nbd` in a separate systemd scope, waits for NBD readiness before mounting, and treats an unreadable mounted disk source as unhealthy during reconcile so broken rootfs mounts are recreated.

`internal/containerd/errors.go` adds a targeted recovery hint for containerd socket permission errors used by direct shell/copy paths.

`internal/daemonstatus/status.go` avoids changing permissions on pre-existing socket parent directories, which lets `--status-socket /tmp/<name>` work.

`internal/hostnet/attach.go` makes container network attach idempotent when `ethN` already exists, preventing the reconciler from deleting and recreating a running container's veth on every tick.

`internal/topologyui/shell.go` now passes a signal-aware context to container shell execution.

`internal/lab/normalize.go` now has compatibility migration for legacy non-UUID node IDs. The migration preserves the legacy ID as the visible node `name`, assigns deterministic UUID node IDs, and rewrites known references. `internal/lab`, `internal/topology`, and `internal/reconciler` now pass with this model.

`internal/topologyui/app.go` and `internal/topologyui/model.go` include compatibility fallbacks so older name-keyed runtime snapshots and graph lookups can still resolve visible nodes after UUID migration.

`internal/topologyui/disk_actions.go` keeps active disks visible by matching the current workload disk path when attachment metadata is stale or legacy-keyed.

`internal/topologyui/app_test.go` and `internal/virt/xml_test.go` now assert the UUID-backed identity model while preserving visible labels such as `vm1`, `web`, `lan`, and `uplink1`.

## Final Status

Manual runtime coverage for the current lab is good for:

- render
- daemon status
- container shell
- VM shell / serial console
- container file copy
- VM file copy failure mode when guest agent is absent
- container NAT through OpenVPN
- VM state and VNC target
- first-run default lab creation
- key CLI error paths

Additional isolated/high-level coverage is good for:

- TUI command parser create/set/delete flows
- context menus, top ribbon, move mode, connect mode, disk menu, shell setup, and VNC setup
- topology service mutation semantics and rollback behavior
- isolated default-lab creation and narrow no-raw render
- install layout, direct lab path selection, foxlabd empty-lab `--once`/`--destroy`, and status socket startup in an existing directory
- legacy `.lab` loading, validation failures, duplicate visible-name guards, lab-path parser conflicts, and unsupported status-socket commands

Current automated gates are green:

- `GOCACHE=/tmp/foxlab-cli-go-build go test ./...`
- `make build`
- `make smoke`
- `git diff --check`

Remaining risk is operational rather than test-suite red: containerd-backed CLI actions still require root or socket access on this host, but they now say how to recover. The existing Kali qcow2 should remain under observation because earlier manual testing saw guest rootfs I/O errors before the current `/bin/sh` and copy checks passed.
