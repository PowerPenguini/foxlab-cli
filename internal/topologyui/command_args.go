package topologyui

import (
	"fmt"
	"strconv"
	"strings"
)

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
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate argument: %s", key)
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
