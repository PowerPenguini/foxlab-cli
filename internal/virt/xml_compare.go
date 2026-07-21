package virt

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"foxlab-cli/internal/lab"
)

type comparableDomain struct {
	MemoryKiB int64
	CPUs      int
	Disk      comparableDisk
	ISO       comparableDisk
	VNC       bool
	Tablet    bool
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
		Inputs []struct {
			Type string `xml:"type,attr"`
		} `xml:"input"`
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
	out := comparableDomain{MemoryKiB: int64(data.MemoryMB) * 1024, CPUs: data.CPUs, VNC: data.HasVNC, Tablet: data.HasTablet}
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
	for _, input := range parsed.Devices.Inputs {
		if input.Type == "tablet" {
			out.Tablet = true
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
