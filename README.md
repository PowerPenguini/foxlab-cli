# FoxLab CLI

FoxLab CLI is a terminal topology app and runtime helper for editing `.lab`
network labs and converging VM/container workloads through libvirt and
containerd backends.

## Showcase

![FoxLab default lab topology TUI showcase](docs/showcase/foxlab-default-topology.png)

The showcase image is a real terminal screenshot of an interactive `foxlab`
session opened on the default lab at `~/.foxlab/default.lab`.
Render the same lab as one non-interactive smoke frame with:

```sh
GOCACHE=/tmp/foxlab-cli-go-build GOPROXY=off go run ./cmd/foxlab --no-raw --width 140 --height 36
```

## Usage

Open the default lab:

```sh
foxlab
```

Open a lab file:

```sh
foxlab --lab path/to/topology.lab
```

Run one non-interactive frame for smoke checks:

```sh
foxlab --no-raw --width 140 --height 36
```

## VNC clipboard

VMs with `vnc: true` expose a bidirectional text clipboard to TigerVNC through
QEMU's vdagent channel. The guest must have `spice-vdagent` installed and
running in its graphical session (or SPICE Guest Tools on Windows). Restart the
VM after enabling VNC so libvirt can add the channel.

## DHCP container

Add a managed DHCP node from the palette with `add dhcp`, or run:

```text
add dhcp dhcp-1 switch=lan
```

Build and import FoxLab's local DHCP image once:

```sh
make dhcp-image
```

The final `foxlab.local/dhcp:2.93` image is built `FROM scratch` and contains
only a statically linked `/dnsmasq` binary. It has no shell, package manager,
CA bundle, passwd database, or base distribution filesystem.

The node is a containerd-backed dnsmasq service attached to one NAT switch.
FoxLab reserves `.2` for the server, keeps `.20` through `.99` for statically
addressed containers, and leases `.100` through `.254` to DHCP clients. The
switch gateway remains `.1`. Only one DHCP container is allowed per NAT switch,
and FoxLab starts it before VMs during reconciliation. Its image, command,
shell, environment, capabilities, MAC, disks, and NIC count are runtime-managed;
the TUI exposes only power, name, NAT-switch selection, move, and delete.

## Lab identifiers

Node identifiers in `.lab` files are mnemonic names used by references throughout
the topology. They must start with a letter or number and contain only letters,
numbers, `_`, or `-`. UUID node identifiers are not supported.

```yaml
name: demo
vms:
  - id: victim-a
    memoryMB: 2048
    cpus: 2
    disk: ""
    networks:
      - switch: lan
containers:
  - id: kali
    image: docker.io/kalilinux/kali-rolling:latest
    networks:
      - switch: lan
switches:
  - id: lan
    mode: bridge
```

The optional node `name` is only a display alias. Creating or renaming a node
through FoxLab uses the supplied mnemonic value as its durable `id`; a rename
therefore updates references and recreates the corresponding runtime resource.
