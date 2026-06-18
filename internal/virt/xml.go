package virt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"foxlab-cli/internal/lab"
)

type domainXMLData struct {
	LabID    string
	VMID     string
	Name     string
	MemoryMB int
	CPUs     int
	HasDisk  bool
	DiskPath string
	DiskType string
	ISO      string
	HasISO   bool
	HasVNC   bool
	Networks []domainNetworkXMLData
}

type domainNetworkXMLData struct {
	Kind       string
	SourceName string
	MAC        string
}

type networkXMLData struct {
	LabID           string
	ID              string
	Name            string
	Bridge          string
	HostBridge      string
	UplinkInterface string
	NAT             bool
	NATInterface    string
	NATAddress      string
	NATNetmask      string
	DHCPStart       string
	DHCPEnd         string
}

func domainXML(l *lab.Lab, vm lab.VM) (string, error) {
	data := domainXMLData{
		LabID:    l.ID,
		VMID:     vm.ID,
		Name:     l.ManagedDomainName(vm),
		MemoryMB: vm.MemoryMB,
		CPUs:     vm.CPUs,
		ISO:      l.ResolvePath(vm.ISO),
		HasISO:   vm.ISO != "",
		HasVNC:   vm.VNC,
	}
	if diskPath, ok := resolveExistingDisk(l, vm.Disk); ok {
		data.HasDisk = true
		data.DiskPath = diskPath
		data.DiskType = detectImageFormat(vm.Disk)
	}
	for index, nic := range vm.Networks {
		linked := vmNICHasNetworkLink(l, vm.ID, index)
		switch {
		case nic.Switch != "" && nic.ExternalLink != "":
			return "", fmt.Errorf("vm %q network references both switch %q and external link %q", vm.ID, nic.Switch, nic.ExternalLink)
		case linked && (nic.Switch != "" || nic.ExternalLink != ""):
			return "", fmt.Errorf("vm %q network nic %d has both direct link and endpoint", vm.ID, index)
		case linked:
			link, _ := findNetworkLinkForEndpoint(l, lab.NetworkEndpoint{Type: "vm", ID: vm.ID, NIC: index})
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       "bridge",
				SourceName: l.ManagedNetworkLinkBridgeName(link),
				MAC:        nic.MAC,
			})
		case nic.Switch != "":
			sw, ok := findSwitch(l, nic.Switch)
			if !ok {
				return "", fmt.Errorf("vm %q references missing switch %q", vm.ID, nic.Switch)
			}
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       "bridge",
				SourceName: l.ManagedSwitchBridgeName(sw),
				MAC:        nic.MAC,
			})
		case nic.ExternalLink != "":
			link, ok := findExternalLink(l, nic.ExternalLink)
			if !ok {
				return "", fmt.Errorf("vm %q references missing external link %q", vm.ID, nic.ExternalLink)
			}
			kind := "direct"
			if isLinuxBridge(link.Interface) {
				kind = "bridge"
			}
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       kind,
				SourceName: link.Interface,
				MAC:        nic.MAC,
			})
		default:
			continue
		}
	}
	var buf bytes.Buffer
	if err := domainTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func networkXML(l *lab.Lab, sw lab.Switch) (string, error) {
	name := l.ManagedNetworkName(sw)
	data := networkXMLData{
		LabID:  l.ID,
		ID:     sw.ID,
		Name:   name,
		Bridge: l.ManagedSwitchBridgeName(sw),
	}
	if sw.Mode == "nat" {
		address, start, end := natIPv4Range(l.ID, sw.ID)
		data.NAT = true
		data.NATAddress = address
		data.NATNetmask = "255.255.255.0"
		data.DHCPStart = start
		data.DHCPEnd = end
	}
	if sw.ExternalLink != "" && sw.Mode != "macnat-bridge" {
		link, ok := findExternalLink(l, sw.ExternalLink)
		if !ok {
			return "", fmt.Errorf("switch %q references missing external link %q", sw.ID, sw.ExternalLink)
		}
		if sw.Mode == "nat" {
			data.NATInterface = link.Interface
		} else if isLinuxBridge(link.Interface) {
			data.HostBridge = link.Interface
		} else {
			data.UplinkInterface = link.Interface
		}
	}
	var buf bytes.Buffer
	if err := networkTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func isManagedXML(xmlText, labID string) bool {
	return strings.Contains(xmlText, `xmlns:foxlab="https://foxlab.local/metadata"`) &&
		strings.Contains(xmlText, `lab="`+labID+`"`)
}

func parseVNCPort(xmlText string) int {
	type graphics struct {
		Type string `xml:"type,attr"`
		Port int    `xml:"port,attr"`
	}
	type devices struct {
		Graphics []graphics `xml:"graphics"`
	}
	type domain struct {
		Devices devices `xml:"devices"`
	}
	var parsed domain
	if err := xml.Unmarshal([]byte(xmlText), &parsed); err != nil {
		return 0
	}
	for _, g := range parsed.Devices.Graphics {
		if g.Type == "vnc" && g.Port > 0 {
			return g.Port
		}
	}
	return 0
}

func findSwitch(l *lab.Lab, id string) (lab.Switch, bool) {
	for _, sw := range l.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return lab.Switch{}, false
}

func findExternalLink(l *lab.Lab, id string) (lab.ExternalLink, bool) {
	for _, link := range l.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
}

func vmNICHasNetworkLink(l *lab.Lab, id string, index int) bool {
	for _, link := range l.NetworkLinks {
		for _, endpoint := range []lab.NetworkEndpoint{link.From, link.To} {
			if endpoint.Type == "vm" && endpoint.ID == id && endpoint.NIC == index {
				return true
			}
		}
	}
	return false
}

func findNetworkLinkForEndpoint(l *lab.Lab, endpoint lab.NetworkEndpoint) (lab.NetworkLink, bool) {
	if l == nil {
		return lab.NetworkLink{}, false
	}
	for _, link := range l.NetworkLinks {
		if sameNetworkEndpoint(link.From, endpoint) || sameNetworkEndpoint(link.To, endpoint) {
			return link, true
		}
	}
	return lab.NetworkLink{}, false
}

func sameNetworkEndpoint(a, b lab.NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}

func detectImageFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".qcow2", ".qcow":
		return "qcow2"
	default:
		return "raw"
	}
}

func resolveExistingDisk(l *lab.Lab, disk string) (string, bool) {
	disk = strings.TrimSpace(disk)
	if disk == "" {
		return "", false
	}
	path := l.ResolvePath(disk)
	if !pathExists(path) {
		return "", false
	}
	return path, true
}

func isLinuxBridge(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "br") || strings.HasPrefix(lower, "virbr") || lower == "docker0" {
		return true
	}
	return pathExists(filepath.Join("/sys/class/net", name, "bridge"))
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func natIPv4Range(labID, switchID string) (address, start, end string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(labID + "/" + switchID))
	octet := 16 + int(h.Sum32()%200)
	prefix := fmt.Sprintf("172.31.%d", octet)
	return prefix + ".1", prefix + ".100", prefix + ".254"
}

var domainTemplate = template.Must(template.New("domain").Parse(`<?xml version="1.0"?>
<domain type="kvm">
  <name>{{ .Name }}</name>
  <metadata>
    <foxlab:resource xmlns:foxlab="https://foxlab.local/metadata" lab="{{ .LabID }}" id="{{ .VMID }}" kind="domain"/>
  </metadata>
  <memory unit="MiB">{{ .MemoryMB }}</memory>
  <currentMemory unit="MiB">{{ .MemoryMB }}</currentMemory>
  <vcpu>{{ .CPUs }}</vcpu>
  <os>
    <type arch="x86_64" machine="q35">hvm</type>
    {{- if .HasISO }}
    <boot dev="cdrom"/>
    {{- else if .HasDisk }}
    <boot dev="hd"/>
    {{- end }}
  </os>
  <features>
    <acpi/>
    <apic/>
    <vmport state="off"/>
  </features>
  <devices>
    <emulator>/usr/bin/qemu-system-x86_64</emulator>
    {{- if .HasDisk }}
    <disk type="file" device="disk">
      <driver name="qemu" type="{{ .DiskType }}"/>
      <source file="{{ .DiskPath }}"/>
      <target dev="vda" bus="virtio"/>
    </disk>
    {{- end }}
    {{- if .HasISO }}
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="{{ .ISO }}"/>
      <target dev="sda" bus="sata"/>
      <readonly/>
    </disk>
    {{- end }}
    {{- range .Networks }}
    <interface type="{{ .Kind }}">
      {{- if eq .Kind "bridge" }}
      <source bridge="{{ .SourceName }}"/>
      {{- else if eq .Kind "direct" }}
      <source dev="{{ .SourceName }}" mode="bridge"/>
      {{- end }}
      {{- if .MAC }}
      <mac address="{{ .MAC }}"/>
      {{- end }}
      <model type="virtio"/>
    </interface>
    {{- end }}
    {{- if .HasVNC }}
    <graphics type="vnc" port="-1" autoport="yes" listen="127.0.0.1"/>
    {{- end }}
    <serial type="pty">
      <target type="isa-serial" port="0"/>
    </serial>
    <console type="pty">
      <target type="serial" port="0"/>
    </console>
  </devices>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
</domain>`))

var networkTemplate = template.Must(template.New("network").Parse(`<?xml version="1.0"?>
<network>
  <name>{{ .Name }}</name>
  <metadata>
    <foxlab:resource xmlns:foxlab="https://foxlab.local/metadata" lab="{{ .LabID }}" id="{{ .ID }}" kind="network"/>
  </metadata>
  {{- if .HostBridge }}
  <forward mode="bridge"/>
  <bridge name="{{ .HostBridge }}"/>
  {{- else if .UplinkInterface }}
  <forward mode="bridge">
    <interface dev="{{ .UplinkInterface }}"/>
  </forward>
  {{- else if .NAT }}
  {{- if .NATInterface }}
  <forward mode="nat" dev="{{ .NATInterface }}"/>
  {{- else }}
  <forward mode="nat"/>
  {{- end }}
  <bridge name="{{ .Bridge }}" stp="on" delay="0"/>
  <ip address="{{ .NATAddress }}" netmask="{{ .NATNetmask }}">
    <dhcp>
      <range start="{{ .DHCPStart }}" end="{{ .DHCPEnd }}"/>
    </dhcp>
  </ip>
  {{- else }}
  <bridge name="{{ .Bridge }}" stp="on" delay="0"/>
  {{- end }}
</network>`))
