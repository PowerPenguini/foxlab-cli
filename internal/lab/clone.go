package lab

func Clone(l *Lab) *Lab {
	if l == nil {
		return nil
	}
	out := *l
	out.VMs = cloneVMs(l.VMs)
	out.Containers = cloneContainers(l.Containers)
	out.Switches = cloneSwitches(l.Switches)
	out.ExternalLinks = append([]ExternalLink(nil), l.ExternalLinks...)
	out.NetworkLinks = append([]NetworkLink(nil), l.NetworkLinks...)
	out.Disks = append([]Disk(nil), l.Disks...)
	out.Layout.Nodes = clonePositionMap(l.Layout.Nodes)
	out.Layout.Links = append([]LayoutLink(nil), l.Layout.Links...)
	out.Meta = cloneStringMap(l.Meta)
	return &out
}

func cloneSwitches(in []Switch) []Switch {
	out := append([]Switch(nil), in...)
	for i := range out {
		out[i].ExternalLinks = append([]string(nil), in[i].ExternalLinks...)
	}
	return out
}

func cloneVMs(in []VM) []VM {
	out := append([]VM(nil), in...)
	for i := range out {
		out[i].Networks = append([]VMNetwork(nil), in[i].Networks...)
	}
	return out
}

func cloneContainers(in []Container) []Container {
	out := append([]Container(nil), in...)
	for i := range out {
		out[i].Command = append([]string(nil), in[i].Command...)
		out[i].Env = cloneStringMap(in[i].Env)
		out[i].Networks = append([]ContainerNetwork(nil), in[i].Networks...)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func clonePositionMap(in map[string]Position) map[string]Position {
	if in == nil {
		return nil
	}
	out := make(map[string]Position, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
