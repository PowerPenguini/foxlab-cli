package topologyui

type commandSpec struct {
	Name      string
	Aliases   []string
	Usage     string
	HelpTopic string
}

var commandCatalog = []commandSpec{
	{Name: "quit", Aliases: []string{"q"}, Usage: "quit [all]"},
	{Name: "help", Aliases: []string{"h"}, Usage: "help [topic]"},
	{Name: "add", Usage: "add <vm|container|dhcp|switch|disk|uplink>", HelpTopic: "add"},
	{Name: "vm", Usage: "vm <create|set|start|stop|nic|delete> ...", HelpTopic: "vm"},
	{Name: "container", Aliases: []string{"ct"}, Usage: "container <create|set|start|stop|nic|delete> ...", HelpTopic: "container"},
	{Name: "disk", Usage: "disk <create|attach|detach|merge|resize|info|delete|layer> ...", HelpTopic: "disk"},
	{Name: "shell", Usage: "shell <vm|container> <id>", HelpTopic: "tabs"},
	{Name: "tabnext", Usage: "tabnext", HelpTopic: "tabs"},
	{Name: "tabprev", Usage: "tabprev", HelpTopic: "tabs"},
	{Name: "tabclose", Usage: "tabclose [index|label]", HelpTopic: "tabs"},
	{Name: "tabrestart", Usage: "tabrestart [index|label]", HelpTopic: "tabs"},
	{Name: "switch", Aliases: []string{"sw"}, Usage: "switch <create|set|delete> ...", HelpTopic: "switch"},
	{Name: "uplink", Aliases: []string{"up", "external", "ext"}, Usage: "uplink <create|set|delete> ...", HelpTopic: "uplink"},
	{Name: "link", Aliases: []string{"links"}, Usage: "link <add|delete> ...", HelpTopic: "link"},
}

func resolveCommandSpec(name string) (commandSpec, bool) {
	for _, spec := range commandCatalog {
		if name == spec.Name {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if name == alias {
				return spec, true
			}
		}
	}
	return commandSpec{}, false
}
