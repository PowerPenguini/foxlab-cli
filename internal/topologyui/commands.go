package topologyui

import (
	"fmt"
	"strconv"
	"strings"
)

func (a *App) executeCommand(command string) bool {
	if command == "" {
		return false
	}
	fields, err := commandFields(command)
	if err != nil {
		a.State.Message = err.Error()
		return false
	}
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "q", "quit":
		return true
	case "help", "h":
		a.State.Console = helpLines(commandArg(fields, 1))
		a.State.Message = ""
	case "vm":
		a.executeVMCommand(fields)
	case "switch", "sw":
		a.executeSwitchCommand(fields)
	case "external", "ext":
		a.executeExternalCommand(fields)
	default:
		a.State.Message = "unknown command: " + fields[0]
	}
	return false
}

func (a *App) executeSwitchCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: switch <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: switch create <id> [mode=bridge|nat|macnat-bridge] [external=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.switchCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: switch set <id> mode=bridge external=ID"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.switchSet(fields[2], args)
	case "delete", "rm":
		a.switchDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown switch command: " + fields[1]
	}
}

func (a *App) executeExternalCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: external <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: external create <id> interface=IFACE"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.externalCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: external set <id> interface=IFACE name=NAME"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.externalSet(fields[2], args)
	case "delete", "rm":
		a.externalDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown external command: " + fields[1]
	}
}

func (a *App) executeVMCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: vm <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: vm create <id> [cpus=N] [memory=N] [switch=ID|external=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.vmCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: vm set <id> cpus=N memory=N name=NAME"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.vmSet(fields[2], args)
	case "delete", "rm":
		a.vmDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown vm command: " + fields[1]
	}
}

func parseArgs(fields []string) (map[string]string, error) {
	out := map[string]string{}
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return nil, fmt.Errorf("expected key=value: %q", field)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			return nil, fmt.Errorf("expected key=value: %q", field)
		}
		if strings.HasSuffix(key, "+") || strings.HasSuffix(key, "-") {
			return nil, fmt.Errorf("unsupported increment syntax: %q", key)
		}
		out[key] = strings.TrimSpace(unquoteValue(value))
	}
	return out, nil
}

func unexpectedVMCreateArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"cpus":     {},
		"memory":   {},
		"mem":      {},
		"disk":     {},
		"switch":   {},
		"external": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMSetArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"disk":     {},
		"iso":      {},
		"vnc":      {},
		"cpus":     {},
		"memory":   {},
		"mem":      {},
		"switch":   {},
		"external": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func commandFields(command string) ([]string, error) {
	var fields []string
	var b strings.Builder
	quote := rune(0)
	escaped := false
	for _, ch := range command {
		switch {
		case escaped:
			b.WriteRune(ch)
			escaped = false
		case ch == '\\' && quote != 0:
			escaped = true
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				b.WriteRune(ch)
			}
		case ch == '"' || ch == '\'':
			quote = ch
		case ch == ' ' || ch == '\t':
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(ch)
		}
	}
	if escaped {
		b.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields, nil
}

func unquoteValue(value string) string {
	if len(value) < 2 {
		return value
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}

func commandValue(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\"'\\") {
		return strconv.Quote(value)
	}
	return value
}

func commandArg(fields []string, index int) string {
	if len(fields) <= index {
		return ""
	}
	return fields[index]
}

func intArg(args map[string]string, key string, fallback int) int {
	if value, ok := positiveInt(args[key]); ok {
		return value
	}
	return fallback
}

func positiveInt(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func boolArg(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func helpLines(topic string) []string {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "", "general", "all":
		return []string{
			"help: Space opens menu; : opens console; Enter selects; :q quits",
			"topics: :help vm  :help switch  :help external",
			"history: Up/Down recall console commands; Escape closes console/menu",
			"ids: use vm/switch/external ids or node labels",
		}
	case "vm", "vms":
		return []string{
			"vm create: :vm create <id> cpus=N memory=N [switch=ID] [external=ID]",
			"vm set: :vm set <id> name=.. cpus=N memory=N disk=<path> iso=<path> vnc=true/false",
			"vm delete: :vm delete <id>",
		}
	case "switch", "switches":
		return []string{
			"switch create: :switch create <id> mode=bridge|nat|macnat-bridge external=ID",
			"switch set: :switch set <id> mode=bridge external=ID",
			"switch delete: :switch delete <id>",
		}
	case "external":
		return []string{
			"external create: :external create <id> interface=IFACE",
			"external set: :external set <id> interface=IFACE name=LABEL",
			"external delete: :external delete <id>",
		}
	default:
		return []string{
			"unknown help topic: " + topic,
			"topics: :help vm  :help switch  :help external",
		}
	}
}
