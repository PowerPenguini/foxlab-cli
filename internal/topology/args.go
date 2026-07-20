package topology

import (
	"sort"
	"strconv"
	"strings"
)

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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
	sort.Strings(invalid)
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
