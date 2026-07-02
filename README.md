# FoxLab CLI

FoxLab CLI is a terminal topology app and runtime helper for editing `.lab`
network labs and converging VM/container workloads through libvirt and
containerd backends.

## Showcase

![FoxLab topology TUI showcase](docs/showcase/foxlab-topology.png)

The showcase image is generated from the deterministic mock topology:

```sh
GOCACHE=/tmp/foxlab-cli-go-build GOPROXY=off go run ./cmd/foxlab --mock --no-raw --width 90 --height 24
```

## Usage

Render the built-in mock topology:

```sh
foxlab --mock
```

Open a lab file:

```sh
foxlab --lab path/to/topology.lab
```

Run one non-interactive frame for smoke checks:

```sh
foxlab --mock --no-raw --width 90 --height 24
```
