package topology

import (
	"strconv"
	"strings"
)

func unexpectedVMCreateArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"cpus":     {},
		"memory":   {},
		"mem":      {},
		"disk":     {},
		"switch":   {},
		"external": {},
		"uplink":   {},
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
		"uplink":   {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMNICAddArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"mac": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMNICConnectArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"to":       {},
		"target":   {},
		"switch":   {},
		"external": {},
		"uplink":   {},
		"mac":      {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedContainerNICAddArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"mac": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedContainerNICConnectArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"to":       {},
		"target":   {},
		"switch":   {},
		"external": {},
		"uplink":   {},
		"mac":      {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedSwitchArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":         {},
		"mode":         {},
		"external":     {},
		"externallink": {},
		"uplink":       {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedExternalArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":      {},
		"interface": {},
		"mode":      {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedDiskCreateArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"size":   {},
		"format": {},
		"to":     {},
		"target": {},
		"attach": {},
	}
	return unexpectedArgs(args, valid)
}

func unexpectedDiskAttachArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"to":     {},
		"target": {},
	}
	return unexpectedArgs(args, valid)
}

func unexpectedDiskDetachArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"type":   {},
		"from":   {},
		"target": {},
		"disk":   {},
	}
	return unexpectedArgs(args, valid)
}

func unexpectedDiskResizeArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"size":  {},
		"force": {},
	}
	return unexpectedArgs(args, valid)
}

func unexpectedArgs(args map[string]string, valid map[string]struct{}) []string {
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func nicIndexArg(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
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

func positiveIntField(args map[string]string, key string) (int, bool, bool) {
	value, present := args[key]
	if !present {
		return 0, false, true
	}
	parsed, ok := positiveInt(value)
	return parsed, true, ok
}

func parseBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
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

func unexpectedContainerArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"image":    {},
		"disk":     {},
		"command":  {},
		"env":      {},
		"switch":   {},
		"external": {},
		"uplink":   {},
		"mac":      {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func splitCommand(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Fields(value)
}

func parseEnv(value string) map[string]string {
	if value == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(key) != "" {
			out[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
