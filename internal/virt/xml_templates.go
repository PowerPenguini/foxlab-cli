package virt

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"strings"
	"text/template"
)

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
  {{- if .UUID }}
  <uuid>{{ xmltext .UUID }}</uuid>
  {{- end }}
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
    {{- if .HasTablet }}
    <input type="tablet" bus="usb"/>
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
