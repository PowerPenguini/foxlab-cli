package lab

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ManagedPrefix = "foxlab"
	FileExtension = ".lab"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

type Lab struct {
	ID            string            `json:"id" yaml:"id"`
	VMs           []VM              `json:"vms,omitempty" yaml:"vms,omitempty"`
	Containers    []Container       `json:"containers,omitempty" yaml:"containers,omitempty"`
	Switches      []Switch          `json:"switches,omitempty" yaml:"switches,omitempty"`
	ExternalLinks []ExternalLink    `json:"externalLinks,omitempty" yaml:"externalLinks,omitempty"`
	NetworkLinks  []NetworkLink     `json:"networkLinks,omitempty" yaml:"networkLinks,omitempty"`
	Disks         []Disk            `json:"disks,omitempty" yaml:"disks,omitempty"`
	Layout        Layout            `json:"layout,omitempty" yaml:"layout,omitempty"`
	Meta          map[string]string `json:"meta,omitempty" yaml:"meta,omitempty"`

	path string
	root string
}

type Disk struct {
	ID     string `json:"id" yaml:"id"`
	Path   string `json:"path" yaml:"path"`
	SizeGB int    `json:"sizeGB,omitempty" yaml:"sizeGB,omitempty"`
	Format string `json:"format,omitempty" yaml:"format,omitempty"`
}

type VM struct {
	ID       string      `json:"id" yaml:"id"`
	Name     string      `json:"name,omitempty" yaml:"name,omitempty"`
	MemoryMB int         `json:"memoryMB" yaml:"memoryMB"`
	CPUs     int         `json:"cpus" yaml:"cpus"`
	Disk     string      `json:"disk" yaml:"disk"`
	ISO      string      `json:"iso,omitempty" yaml:"iso,omitempty"`
	VNC      bool        `json:"vnc,omitempty" yaml:"vnc,omitempty"`
	Networks []VMNetwork `json:"networks,omitempty" yaml:"networks,omitempty"`
}

type VMNetwork struct {
	Switch       string `json:"switch,omitempty" yaml:"switch,omitempty"`
	ExternalLink string `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
	MAC          string `json:"mac,omitempty" yaml:"mac,omitempty"`
}

type Container struct {
	ID       string             `json:"id" yaml:"id"`
	Name     string             `json:"name,omitempty" yaml:"name,omitempty"`
	Image    string             `json:"image" yaml:"image"`
	Command  []string           `json:"command,omitempty" yaml:"command,omitempty"`
	Env      map[string]string  `json:"env,omitempty" yaml:"env,omitempty"`
	Networks []ContainerNetwork `json:"networks,omitempty" yaml:"networks,omitempty"`
}

type ContainerNetwork struct {
	Switch string `json:"switch,omitempty" yaml:"switch,omitempty"`
	MAC    string `json:"mac,omitempty" yaml:"mac,omitempty"`
}

type Switch struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name,omitempty" yaml:"name,omitempty"`
	Mode         string `json:"mode" yaml:"mode"`
	ExternalLink string `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
}

type ExternalLink struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Interface string `json:"interface" yaml:"interface"`
}

type NetworkLink struct {
	From NetworkEndpoint `json:"from" yaml:"from"`
	To   NetworkEndpoint `json:"to" yaml:"to"`
}

type NetworkEndpoint struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
	NIC  int    `json:"nic" yaml:"nic"`
}

type Layout struct {
	Nodes map[string]Position `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Links []LayoutLink        `json:"links,omitempty" yaml:"links,omitempty"`
}

type LayoutLink struct {
	From LayoutEndpoint `json:"from" yaml:"from"`
	To   LayoutEndpoint `json:"to" yaml:"to"`
}

type LayoutEndpoint struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
}

type Position struct {
	X int `json:"x" yaml:"x"`
	Y int `json:"y" yaml:"y"`
}

func LoadFile(path string) (*Lab, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var l Lab
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	l.path = abs
	l.root = filepath.Dir(abs)
	l.Normalize()
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func SaveFile(path string, l *Lab) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	copy := *l
	copy.path = abs
	copy.root = filepath.Dir(abs)
	copy.Normalize()
	if err := copy.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&copy)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

func ListFiles(workspace string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != workspace {
				return filepath.SkipDir
			}
			return nil
		}
		if isLabFile(path) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func isLabFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == FileExtension
}

func (l *Lab) Normalize() {
	l.ID = strings.TrimSpace(l.ID)
	for i := range l.VMs {
		l.VMs[i].ID = strings.TrimSpace(l.VMs[i].ID)
		l.VMs[i].Name = strings.TrimSpace(l.VMs[i].Name)
		l.VMs[i].Disk = strings.TrimSpace(l.VMs[i].Disk)
		l.VMs[i].ISO = strings.TrimSpace(l.VMs[i].ISO)
		for j := range l.VMs[i].Networks {
			l.VMs[i].Networks[j].Switch = strings.TrimSpace(l.VMs[i].Networks[j].Switch)
			l.VMs[i].Networks[j].ExternalLink = strings.TrimSpace(l.VMs[i].Networks[j].ExternalLink)
			l.VMs[i].Networks[j].MAC = strings.TrimSpace(l.VMs[i].Networks[j].MAC)
		}
		if l.VMs[i].MemoryMB == 0 {
			l.VMs[i].MemoryMB = 2048
		}
		if l.VMs[i].CPUs == 0 {
			l.VMs[i].CPUs = 2
		}
	}
	for i := range l.Containers {
		l.Containers[i].ID = strings.TrimSpace(l.Containers[i].ID)
		l.Containers[i].Name = strings.TrimSpace(l.Containers[i].Name)
		l.Containers[i].Image = strings.TrimSpace(l.Containers[i].Image)
		for j := range l.Containers[i].Command {
			l.Containers[i].Command[j] = strings.TrimSpace(l.Containers[i].Command[j])
		}
		for j := range l.Containers[i].Networks {
			l.Containers[i].Networks[j].Switch = strings.TrimSpace(l.Containers[i].Networks[j].Switch)
			l.Containers[i].Networks[j].MAC = strings.TrimSpace(l.Containers[i].Networks[j].MAC)
		}
	}
	for i := range l.Switches {
		l.Switches[i].ID = strings.TrimSpace(l.Switches[i].ID)
		l.Switches[i].Name = strings.TrimSpace(l.Switches[i].Name)
		l.Switches[i].Mode = strings.TrimSpace(l.Switches[i].Mode)
		l.Switches[i].ExternalLink = strings.TrimSpace(l.Switches[i].ExternalLink)
		if l.Switches[i].ExternalLink != "" && l.Switches[i].Mode == "" {
			l.Switches[i].Mode = "bridge"
		}
		if l.Switches[i].Mode == "" {
			l.Switches[i].Mode = "bridge"
		}
	}
	for i := range l.ExternalLinks {
		l.ExternalLinks[i].ID = strings.TrimSpace(l.ExternalLinks[i].ID)
		l.ExternalLinks[i].Name = strings.TrimSpace(l.ExternalLinks[i].Name)
		l.ExternalLinks[i].Interface = strings.TrimSpace(l.ExternalLinks[i].Interface)
	}
	for i := range l.NetworkLinks {
		l.NetworkLinks[i].From.Type = strings.ToLower(strings.TrimSpace(l.NetworkLinks[i].From.Type))
		l.NetworkLinks[i].From.ID = strings.TrimSpace(l.NetworkLinks[i].From.ID)
		l.NetworkLinks[i].To.Type = strings.ToLower(strings.TrimSpace(l.NetworkLinks[i].To.Type))
		l.NetworkLinks[i].To.ID = strings.TrimSpace(l.NetworkLinks[i].To.ID)
	}
}

func (l *Lab) Validate() error {
	var problems []string
	if !validID(l.ID) {
		problems = append(problems, "lab id must start with a letter/number and contain only letters, numbers, '_' or '-'")
	}

	switchIDs := map[string]struct{}{}
	for _, sw := range l.Switches {
		if !validID(sw.ID) {
			problems = append(problems, fmt.Sprintf("switch %q has invalid id", sw.ID))
		}
		if _, exists := switchIDs[sw.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate switch id %q", sw.ID))
		}
		switchIDs[sw.ID] = struct{}{}
		if sw.Mode != "bridge" && sw.Mode != "nat" && sw.Mode != "macnat-bridge" {
			problems = append(problems, fmt.Sprintf("switch %q uses unsupported mode %q; supported modes are bridge, nat and macnat-bridge", sw.ID, sw.Mode))
		}
		if sw.Mode == "macnat-bridge" && sw.ExternalLink == "" {
			problems = append(problems, fmt.Sprintf("switch %q macnat-bridge mode requires externalLink", sw.ID))
		}
	}

	externalLinkIDs := map[string]struct{}{}
	for _, link := range l.ExternalLinks {
		if !validID(link.ID) {
			problems = append(problems, fmt.Sprintf("external link %q has invalid id", link.ID))
		}
		if _, exists := externalLinkIDs[link.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate external link id %q", link.ID))
		}
		externalLinkIDs[link.ID] = struct{}{}
		if link.Interface == "" {
			problems = append(problems, fmt.Sprintf("external link %q interface is required", link.ID))
		}
	}

	for _, sw := range l.Switches {
		if sw.ExternalLink == "" {
			continue
		}
		if _, ok := externalLinkIDs[sw.ExternalLink]; !ok {
			problems = append(problems, fmt.Sprintf("switch %q references missing external link %q", sw.ID, sw.ExternalLink))
		}
	}

	vmIDs := map[string]struct{}{}
	for _, vm := range l.VMs {
		if !validID(vm.ID) {
			problems = append(problems, fmt.Sprintf("vm %q has invalid id", vm.ID))
		}
		if _, exists := vmIDs[vm.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate vm id %q", vm.ID))
		}
		vmIDs[vm.ID] = struct{}{}
		if vm.MemoryMB <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q memoryMB must be greater than zero", vm.ID))
		}
		if vm.CPUs <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q cpus must be greater than zero", vm.ID))
		}
		if vm.Disk == "" {
			problems = append(problems, fmt.Sprintf("vm %q disk is required", vm.ID))
		}
		for _, nic := range vm.Networks {
			switchRef := nic.Switch != ""
			externalRef := nic.ExternalLink != ""
			if switchRef && externalRef {
				problems = append(problems, fmt.Sprintf("vm %q network must not reference both switch and externalLink", vm.ID))
				continue
			}
			if switchRef {
				if _, ok := switchIDs[nic.Switch]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing switch %q", vm.ID, nic.Switch))
				}
			}
			if externalRef {
				if _, ok := externalLinkIDs[nic.ExternalLink]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing external link %q", vm.ID, nic.ExternalLink))
				}
			}
		}
	}

	containerIDs := map[string]struct{}{}
	for _, ct := range l.Containers {
		if !validID(ct.ID) {
			problems = append(problems, fmt.Sprintf("container %q has invalid id", ct.ID))
		}
		if _, exists := containerIDs[ct.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate container id %q", ct.ID))
		}
		containerIDs[ct.ID] = struct{}{}
		if ct.Image == "" {
			problems = append(problems, fmt.Sprintf("container %q image is required", ct.ID))
		}
		for _, nic := range ct.Networks {
			if nic.Switch == "" {
				continue
			}
			if _, ok := switchIDs[nic.Switch]; !ok {
				problems = append(problems, fmt.Sprintf("container %q references missing switch %q", ct.ID, nic.Switch))
			}
		}
	}

	linkedNICs := map[string]struct{}{}
	for _, link := range l.NetworkLinks {
		endpoints := []NetworkEndpoint{link.From, link.To}
		if networkEndpointKey(link.From) == networkEndpointKey(link.To) {
			problems = append(problems, "network link endpoints must be different")
			continue
		}
		for _, endpoint := range endpoints {
			key := networkEndpointKey(endpoint)
			if _, exists := linkedNICs[key]; exists {
				problems = append(problems, fmt.Sprintf("network endpoint %s is linked more than once", key))
			}
			switch endpoint.Type {
			case "vm":
				vm, ok := findVMByID(l.VMs, endpoint.ID)
				if !ok {
					problems = append(problems, fmt.Sprintf("network link references missing vm %q", endpoint.ID))
					continue
				}
				if endpoint.NIC < 0 || endpoint.NIC >= len(vm.Networks) {
					problems = append(problems, fmt.Sprintf("network link references missing vm nic %q:%d", endpoint.ID, endpoint.NIC))
					continue
				}
				nic := vm.Networks[endpoint.NIC]
				if nic.Switch != "" || nic.ExternalLink != "" {
					problems = append(problems, fmt.Sprintf("network link endpoint vm %q nic %d is already connected", endpoint.ID, endpoint.NIC))
					continue
				}
			case "container":
				ct, ok := findContainerByID(l.Containers, endpoint.ID)
				if !ok {
					problems = append(problems, fmt.Sprintf("network link references missing container %q", endpoint.ID))
					continue
				}
				if endpoint.NIC < 0 || endpoint.NIC >= len(ct.Networks) {
					problems = append(problems, fmt.Sprintf("network link references missing container nic %q:%d", endpoint.ID, endpoint.NIC))
					continue
				}
				if ct.Networks[endpoint.NIC].Switch != "" {
					problems = append(problems, fmt.Sprintf("network link endpoint container %q nic %d is already connected", endpoint.ID, endpoint.NIC))
					continue
				}
			default:
				problems = append(problems, fmt.Sprintf("network link references unknown endpoint type %q", endpoint.Type))
				continue
			}
			linkedNICs[key] = struct{}{}
		}
	}

	for id := range l.Layout.Nodes {
		if _, ok := vmIDs[id]; ok {
			continue
		}
		if _, ok := switchIDs[id]; ok {
			continue
		}
		if _, ok := externalLinkIDs[id]; ok {
			continue
		}
		if _, ok := containerIDs[id]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf("layout references missing node %q", id))
	}
	for _, link := range l.Layout.Links {
		for _, endpoint := range []LayoutEndpoint{link.From, link.To} {
			switch endpoint.Type {
			case "vm":
				if _, ok := vmIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing vm %q", endpoint.ID))
				}
			case "switch":
				if _, ok := switchIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing switch %q", endpoint.ID))
				}
			case "external":
				if _, ok := externalLinkIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing external link %q", endpoint.ID))
				}
			case "container":
				if _, ok := containerIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing container %q", endpoint.ID))
				}
			default:
				problems = append(problems, fmt.Sprintf("layout link references unknown node type %q", endpoint.Type))
			}
		}
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func (l *Lab) Path() string {
	return l.path
}

func (l *Lab) Root() string {
	if l.root != "" {
		return l.root
	}
	return "."
}

func (l *Lab) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(l.Root(), path))
}

func (l *Lab) ManagedDomainName(vm VM) string {
	return managedName(l.ID, vm.ID)
}

func (l *Lab) ManagedNetworkName(sw Switch) string {
	return managedName(l.ID, sw.ID)
}

func (l *Lab) ManagedSwitchBridgeName(sw Switch) string {
	return bridgeName(l.ManagedNetworkName(sw))
}

func (l *Lab) ManagedContainerName(ct Container) string {
	return managedName(l.ID, ct.ID)
}

func (l *Lab) ManagedNetworkLinkBridgeName(link NetworkLink) string {
	from := networkEndpointKey(link.From)
	to := networkEndpointKey(link.To)
	if to < from {
		from, to = to, from
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(l.ID + "|" + from + "|" + to))
	return fmt.Sprintf("flp2p%08x", h.Sum32())
}

func validID(id string) bool {
	return idPattern.MatchString(id)
}

func managedName(labID, resourceID string) string {
	return strings.ToLower(fmt.Sprintf("%s-%s-%s", ManagedPrefix, labID, resourceID))
}

func networkEndpointKey(endpoint NetworkEndpoint) string {
	return fmt.Sprintf("%s:%s:%d", endpoint.Type, endpoint.ID, endpoint.NIC)
}

func findVMByID(vms []VM, id string) (VM, bool) {
	for _, vm := range vms {
		if vm.ID == id {
			return vm, true
		}
	}
	return VM{}, false
}

func findContainerByID(containers []Container, id string) (Container, bool) {
	for _, ct := range containers {
		if ct.ID == id {
			return ct, true
		}
	}
	return Container{}, false
}

func bridgeName(managedName string) string {
	const maxLinuxIfName = 15
	clean := strings.NewReplacer("_", "", "-", "").Replace(managedName)
	if len(clean) > maxLinuxIfName-2 {
		clean = clean[:maxLinuxIfName-2]
	}
	return "fl" + clean
}
