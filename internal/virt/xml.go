package virt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"

	"foxlab-cli/internal/lab"
)

type domainXMLData struct {
	LabID      string
	VMID       string
	Name       string
	MemoryMB   int
	CPUs       int
	HasDisk    bool
	DiskPath   string
	DiskType   string
	ISO        string
	HasISO     bool
	HasVNC     bool
	ConfigHash string `json:"-"`
	Networks   []domainNetworkXMLData
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
	data, err := desiredDomainXMLData(l, vm)
	if err != nil {
		return "", err
	}
	data.ConfigHash, err = domainConfigHash(data)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := domainTemplate.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func desiredDomainXMLData(l *lab.Lab, vm lab.VM) (domainXMLData, error) {
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
			return domainXMLData{}, fmt.Errorf("vm %q network references both switch %q and external link %q", vm.ID, nic.Switch, nic.ExternalLink)
		case linked && (nic.Switch != "" || nic.ExternalLink != ""):
			return domainXMLData{}, fmt.Errorf("vm %q network nic %d has both direct link and endpoint", vm.ID, index)
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
				return domainXMLData{}, fmt.Errorf("vm %q references missing switch %q", vm.ID, nic.Switch)
			}
			mac := nic.MAC
			if switchUsesMacNAT(l, sw) {
				mac = firstNonEmpty(mac, l.GeneratedNICMAC("vm", vm.ID, index))
			}
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       "bridge",
				SourceName: l.ManagedSwitchBridgeName(sw),
				MAC:        mac,
			})
		case nic.ExternalLink != "":
			link, ok := findExternalLink(l, nic.ExternalLink)
			if !ok {
				return domainXMLData{}, fmt.Errorf("vm %q references missing external link %q", vm.ID, nic.ExternalLink)
			}
			kind := "direct"
			source := link.Interface
			mac := nic.MAC
			if link.Mode == lab.ExternalModeNAT || link.Mode == lab.ExternalModeMacNAT {
				kind = "bridge"
				source = l.ManagedExternalBridgeName(link)
				if link.Mode == lab.ExternalModeMacNAT {
					mac = firstNonEmpty(mac, l.GeneratedNICMAC("vm", vm.ID, index))
				}
			} else if isLinuxBridge(link.Interface) {
				kind = "bridge"
			}
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       kind,
				SourceName: source,
				MAC:        mac,
			})
		default:
			continue
		}
	}
	return data, nil
}

func domainConfigHash(data domainXMLData) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

type comparableDomain struct {
	MemoryKiB int64
	CPUs      int
	Disk      comparableDisk
	ISO       comparableDisk
	VNC       bool
	Networks  []comparableNetwork
}

type comparableDisk struct {
	Present bool
	Path    string
	Format  string
}

type comparableNetwork struct {
	Kind   string
	Source string
	MAC    string
}

type parsedDomainXML struct {
	Metadata struct {
		Resource struct {
			Lab        string `xml:"lab,attr"`
			ID         string `xml:"id,attr"`
			Kind       string `xml:"kind,attr"`
			ConfigHash string `xml:"configSHA256,attr"`
		} `xml:"resource"`
	} `xml:"metadata"`
	Memory struct {
		Value int64  `xml:",chardata"`
		Unit  string `xml:"unit,attr"`
	} `xml:"memory"`
	VCPU    int `xml:"vcpu"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Driver struct {
				Type string `xml:"type,attr"`
			} `xml:"driver"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			Target struct {
				Dev string `xml:"dev,attr"`
			} `xml:"target"`
		} `xml:"disk"`
		Interfaces []struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Bridge string `xml:"bridge,attr"`
				Dev    string `xml:"dev,attr"`
			} `xml:"source"`
			MAC struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
		} `xml:"interface"`
		Graphics []struct {
			Type string `xml:"type,attr"`
		} `xml:"graphics"`
	} `xml:"devices"`
}

func domainConfigMatches(l *lab.Lab, vm lab.VM, liveXML string) (bool, error) {
	desiredData, err := desiredDomainXMLData(l, vm)
	if err != nil {
		return false, err
	}
	desiredHash, err := domainConfigHash(desiredData)
	if err != nil {
		return false, err
	}
	parsed, err := parseDomainXML(liveXML)
	if err != nil {
		return false, err
	}
	if parsed.Metadata.Resource.ConfigHash != "" && parsed.Metadata.Resource.ConfigHash != desiredHash {
		return false, nil
	}
	return reflect.DeepEqual(comparableDomainFromLive(parsed, desiredData), comparableDomainFromDesired(desiredData)), nil
}

func managedDomainMetadata(liveXML string) (labID, id, configHash string, ok bool) {
	parsed, err := parseDomainXML(liveXML)
	if err != nil {
		return "", "", "", false
	}
	resource := parsed.Metadata.Resource
	if resource.Kind != "domain" || resource.Lab == "" || resource.ID == "" {
		return "", "", "", false
	}
	return resource.Lab, resource.ID, resource.ConfigHash, true
}

func parseDomainXML(value string) (parsedDomainXML, error) {
	var parsed parsedDomainXML
	if err := xml.Unmarshal([]byte(value), &parsed); err != nil {
		return parsedDomainXML{}, fmt.Errorf("decode domain XML: %w", err)
	}
	return parsed, nil
}

func comparableDomainFromDesired(data domainXMLData) comparableDomain {
	out := comparableDomain{MemoryKiB: int64(data.MemoryMB) * 1024, CPUs: data.CPUs, VNC: data.HasVNC}
	if data.HasDisk {
		out.Disk = comparableDisk{Present: true, Path: filepath.Clean(data.DiskPath), Format: data.DiskType}
	}
	if data.HasISO {
		out.ISO = comparableDisk{Present: true, Path: filepath.Clean(data.ISO), Format: "raw"}
	}
	for _, network := range data.Networks {
		out.Networks = append(out.Networks, comparableNetwork{Kind: network.Kind, Source: network.SourceName, MAC: strings.ToLower(network.MAC)})
	}
	return out
}

func comparableDomainFromLive(parsed parsedDomainXML, desired domainXMLData) comparableDomain {
	out := comparableDomain{MemoryKiB: memoryToKiB(parsed.Memory.Value, parsed.Memory.Unit), CPUs: parsed.VCPU}
	for _, graphics := range parsed.Devices.Graphics {
		if graphics.Type == "vnc" {
			out.VNC = true
			break
		}
	}
	for _, disk := range parsed.Devices.Disks {
		value := comparableDisk{Present: true, Path: filepath.Clean(disk.Source.File), Format: disk.Driver.Type}
		switch disk.Target.Dev {
		case "vda":
			out.Disk = value
		case "sda":
			out.ISO = value
		}
	}
	for index, network := range parsed.Devices.Interfaces {
		source := network.Source.Dev
		if network.Type == "bridge" {
			source = network.Source.Bridge
		}
		mac := strings.ToLower(network.MAC.Address)
		if index < len(desired.Networks) && desired.Networks[index].MAC == "" {
			mac = ""
		}
		out.Networks = append(out.Networks, comparableNetwork{Kind: network.Type, Source: source, MAC: mac})
	}
	return out
}

func memoryToKiB(value int64, unit string) int64 {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "b", "byte", "bytes":
		return value / 1024
	case "mb", "mib":
		return value * 1024
	case "gb", "gib":
		return value * 1024 * 1024
	default:
		return value
	}
}

func networkXML(l *lab.Lab, sw lab.Switch) (string, error) {
	name := l.ManagedNetworkName(sw)
	data := networkXMLData{
		LabID:  l.ID,
		ID:     sw.ID,
		Name:   name,
		Bridge: l.ManagedSwitchBridgeName(sw),
	}
	if sw.Mode == "nat" && !switchUsesMacNAT(l, sw) {
		address, start, end := natIPv4Range(l.ID, sw.ID)
		data.NAT = true
		data.NATAddress = address
		data.NATNetmask = "255.255.255.0"
		data.DHCPStart = start
		data.DHCPEnd = end
	}
	if externalID := firstSwitchExternalLink(sw); externalID != "" && !switchUsesMacNAT(l, sw) {
		link, ok := findExternalLink(l, externalID)
		if !ok {
			return "", fmt.Errorf("switch %q references missing external link %q", sw.ID, externalID)
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

func switchUsesMacNAT(l *lab.Lab, sw lab.Switch) bool {
	if sw.Mode == "macnat-bridge" {
		return true
	}
	link, ok := findExternalLink(l, firstSwitchExternalLink(sw))
	return ok && link.Mode == lab.ExternalModeMacNAT
}

func firstSwitchExternalLink(sw lab.Switch) string {
	ids := lab.SwitchExternalLinks(sw)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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

func xmlText(value string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(value))
	return buf.String()
}

func xmlAttr(value string) string {
	return xmlAttrEscaper.Replace(value)
}

var xmlAttrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

func natIPv4Range(labID, switchID string) (address, start, end string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(labID + "/" + switchID))
	octet := 16 + int(h.Sum32()%200)
	prefix := fmt.Sprintf("172.31.%d", octet)
	return prefix + ".1", prefix + ".100", prefix + ".254"
}

var xmlTemplateFuncs = template.FuncMap{
	"xmltext": xmlText,
	"xmlattr": xmlAttr,
}

var domainTemplate = template.Must(template.New("domain").Funcs(xmlTemplateFuncs).Parse(`<?xml version="1.0"?>
<domain type="kvm">
  <name>{{ xmltext .Name }}</name>
  <metadata>
    <foxlab:resource xmlns:foxlab="https://foxlab.local/metadata" lab="{{ xmlattr .LabID }}" id="{{ xmlattr .VMID }}" kind="domain" configSHA256="{{ xmlattr .ConfigHash }}"/>
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
      <driver name="qemu" type="{{ xmlattr .DiskType }}"/>
      <source file="{{ xmlattr .DiskPath }}"/>
      <target dev="vda" bus="virtio"/>
    </disk>
    {{- end }}
    {{- if .HasISO }}
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="{{ xmlattr .ISO }}"/>
      <target dev="sda" bus="sata"/>
      <readonly/>
    </disk>
    {{- end }}
    {{- range .Networks }}
    <interface type="{{ xmlattr .Kind }}">
      {{- if eq .Kind "bridge" }}
      <source bridge="{{ xmlattr .SourceName }}"/>
      {{- else if eq .Kind "direct" }}
      <source dev="{{ xmlattr .SourceName }}" mode="bridge"/>
      {{- end }}
      {{- if .MAC }}
      <mac address="{{ xmlattr .MAC }}"/>
      {{- end }}
      <model type="virtio"/>
    </interface>
    {{- end }}
    {{- if .HasVNC }}
    <graphics type="vnc" port="-1" autoport="yes" listen="127.0.0.1"/>
    <video>
      <model type="virtio" heads="1" primary="yes"/>
    </video>
    {{- end }}
    <serial type="pty">
      <target type="isa-serial" port="0"/>
    </serial>
    <console type="pty">
      <target type="serial" port="0"/>
    </console>
    <channel type="unix">
      <target type="virtio" name="org.qemu.guest_agent.0"/>
    </channel>
  </devices>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
</domain>`))

var networkTemplate = template.Must(template.New("network").Funcs(xmlTemplateFuncs).Parse(`<?xml version="1.0"?>
<network>
  <name>{{ xmltext .Name }}</name>
  <metadata>
    <foxlab:resource xmlns:foxlab="https://foxlab.local/metadata" lab="{{ xmlattr .LabID }}" id="{{ xmlattr .ID }}" kind="network"/>
  </metadata>
  {{- if .HostBridge }}
  <forward mode="bridge"/>
  <bridge name="{{ xmlattr .HostBridge }}"/>
  {{- else if .UplinkInterface }}
  <forward mode="bridge">
    <interface dev="{{ xmlattr .UplinkInterface }}"/>
  </forward>
  {{- else if .NAT }}
  {{- if .NATInterface }}
  <forward mode="nat" dev="{{ xmlattr .NATInterface }}"/>
  {{- else }}
  <forward mode="nat"/>
  {{- end }}
  <bridge name="{{ xmlattr .Bridge }}" stp="on" delay="0"/>
  <ip address="{{ xmlattr .NATAddress }}" netmask="{{ xmlattr .NATNetmask }}">
    <dhcp>
      <range start="{{ xmlattr .DHCPStart }}" end="{{ xmlattr .DHCPEnd }}"/>
    </dhcp>
  </ip>
  {{- else }}
  <bridge name="{{ xmlattr .Bridge }}" stp="on" delay="0"/>
  {{- end }}
</network>`))
