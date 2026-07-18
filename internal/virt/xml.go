package virt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

type domainXMLData struct {
	LabID      string
	VMID       string
	Name       string
	UUID       string `json:"-"`
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
	return domainXMLWithUUID(l, vm, "")
}

func domainXMLWithUUID(l *lab.Lab, vm lab.VM, uuid string) (string, error) {
	data, err := desiredDomainXMLData(l, vm)
	if err != nil {
		return "", err
	}
	data.UUID = strings.TrimSpace(uuid)
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
			link, _ := lab.FindNetworkLinkForEndpoint(l, lab.NetworkEndpoint{Type: "vm", ID: vm.ID, NIC: index})
			data.Networks = append(data.Networks, domainNetworkXMLData{
				Kind:       "bridge",
				SourceName: l.ManagedNetworkLinkBridgeName(link),
				MAC:        nic.MAC,
			})
		case nic.Switch != "":
			sw, ok := lab.FindSwitch(l, nic.Switch)
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
			link, ok := lab.FindExternalLink(l, nic.ExternalLink)
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
		link, ok := lab.FindExternalLink(l, externalID)
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

func switchUsesMacNAT(l *lab.Lab, sw lab.Switch) bool {
	if sw.Mode == "macnat-bridge" {
		return true
	}
	link, ok := lab.FindExternalLink(l, firstSwitchExternalLink(sw))
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
