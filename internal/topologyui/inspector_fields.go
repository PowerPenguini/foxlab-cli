package topologyui

import (
	"sort"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
)

const (
	inspectorFieldText             = "text"
	inspectorFieldBool             = "bool"
	inspectorFieldChoice           = "choice"
	inspectorFieldCapabilityPicker = "capability-picker"
	inspectorFieldInterfacePicker  = "interface-picker"
	inspectorFieldPower            = "power"
	inspectorFieldShellAction      = "shell-action"
	inspectorFieldDeleteAction     = "delete-action"
)

const (
	inspectorFieldListY = 8
	inspectorFooterRows = 1
)

type inspectorField struct {
	id         string
	label      string
	value      string
	kind       string
	nodeID     string
	nodeType   string
	capability string
	choices    []string
}

type inspectorPanelRow struct {
	fieldIndex int
	section    string
	buttonPart int
	spacer     bool
}

func inspectorFields(node Node) []inspectorField {
	field := func(id, label, value, kind string) inspectorField {
		return inspectorField{id: id, label: label, value: value, kind: kind, nodeID: node.ID, nodeType: node.Type}
	}
	power := "run"
	if lab.DesiredState(node.DesiredState) == lab.DesiredStateRunning {
		power = "stop"
	}
	fields := []inspectorField{}
	if node.Type == NodeVM || node.Type == NodeContainer {
		fields = append(fields,
			field("desiredState", "Power", power, inspectorFieldPower),
			field("shellAction", "Shell", node.State, inspectorFieldShellAction),
		)
	}
	switch node.Type {
	case NodeVM:
		fields = append(fields,
			field("name", "Name", node.Label, inspectorFieldText),
			field("cpus", "CPU", nodeDetailRawValue(node, "cpu"), inspectorFieldText),
			field("memory", "Memory", strings.TrimSuffix(nodeDetailRawValue(node, "mem"), "M"), inspectorFieldText),
			field("vnc", "VNC", nodeDetailRawValue(node, "vnc"), inspectorFieldBool),
			field("iso", "ISO", nodeDetailRawValue(node, "iso"), inspectorFieldText),
		)
	case NodeContainer:
		fields = append(fields,
			field("name", "Name", node.Label, inspectorFieldText),
			field("image", "Image", nodeDetailRawValue(node, "image"), inspectorFieldText),
			field("command", "Command", nodeDetailRawValue(node, "command"), inspectorFieldText),
			field("shell", "Shell", nodeDetailRawValue(node, "shell"), inspectorFieldText),
			field("env", "Env", containerEnvValue(node), inspectorFieldText),
		)
		enabled := containerCapabilityEnabledMap(node)
		fields = append(fields, field("capabilities", "Capabilities", strconv.Itoa(len(enabled))+" selected", inspectorFieldCapabilityPicker))
	case NodeSwitch:
		fields = append(fields,
			field("name", "Name", node.Label, inspectorFieldText),
			inspectorField{id: "mode", label: "Mode", value: nodeDetailRawValue(node, "mode"), kind: inspectorFieldChoice, nodeID: node.ID, nodeType: node.Type, choices: []string{"bridge", "nat", "macnat-bridge"}},
		)
	case NodeExternal:
		fields = append(fields,
			field("name", "Name", node.Label, inspectorFieldText),
			field("interface", "Interface", nodeDetailRawValue(node, "interface"), inspectorFieldInterfacePicker),
			inspectorField{id: "mode", label: "Mode", value: nodeDetailRawValue(node, "mode"), kind: inspectorFieldChoice, nodeID: node.ID, nodeType: node.Type, choices: []string{"nat", "direct", "macnat"}},
		)
	}
	if node.Type == NodeVM || node.Type == NodeContainer || node.Type == NodeSwitch || node.Type == NodeExternal {
		fields = append(fields, field("deleteAction", "Delete", "", inspectorFieldDeleteAction))
	}
	return fields
}

func containerEnvValue(node Node) string {
	values := []string{}
	for _, detail := range node.Details {
		key, value, ok := strings.Cut(detail, "=")
		if !ok || !strings.HasPrefix(key, "env.") {
			continue
		}
		values = append(values, strings.TrimPrefix(key, "env.")+"="+value)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}

func inspectorVisibleFieldRows(panel rect) int {
	return max(0, panel.H-inspectorFieldListY-inspectorFooterRows)
}

func inspectorPanelRowsFor(fields []inspectorField) []inspectorPanelRow {
	rows := make([]inspectorPanelRow, 0, len(fields)+2)
	lastSection := ""
	for index, field := range fields {
		if field.kind == inspectorFieldPower || field.kind == inspectorFieldShellAction {
			continue
		}
		if field.kind == inspectorFieldDeleteAction {
			rows = append(rows,
				inspectorPanelRow{fieldIndex: -1, spacer: true},
				inspectorPanelRow{fieldIndex: -1, spacer: true},
				inspectorPanelRow{fieldIndex: index, buttonPart: -1},
				inspectorPanelRow{fieldIndex: index},
				inspectorPanelRow{fieldIndex: index, buttonPart: 1},
			)
			continue
		}
		section := inspectorFieldSection(field)
		if section != lastSection {
			rows = append(rows, inspectorPanelRow{fieldIndex: -1, section: section})
			lastSection = section
		}
		rows = append(rows, inspectorPanelRow{fieldIndex: index})
	}
	return rows
}

func inspectorFieldSection(field inspectorField) string {
	if field.kind == inspectorFieldCapabilityPicker {
		return "LINUX CAPABILITIES"
	}
	switch field.nodeType {
	case NodeVM, NodeContainer:
		return "WORKLOAD"
	case NodeSwitch:
		return "SWITCH"
	case NodeExternal:
		return "UPLINK"
	default:
		return "PROPERTIES"
	}
}

func containerCapabilityEnabledMap(node Node) map[string]bool {
	enabled := map[string]bool{}
	for _, capability := range strings.Split(nodeDetailRawValue(node, "capabilities"), ",") {
		capability = lab.NormalizeContainerCapability(capability)
		if capability != "" {
			enabled[capability] = true
		}
	}
	return enabled
}

func inspectorFieldWindow(panel rect, state ViewState, fields []inspectorField) ([]inspectorPanelRow, int, int) {
	rows := inspectorPanelRowsFor(fields)
	visible := inspectorVisibleFieldRows(panel)
	selectedField := normalizedMenuSelection(state.InspectorSelected, len(fields))
	selectedRow := 0
	for index, row := range rows {
		if row.fieldIndex == selectedField {
			selectedRow = index
			break
		}
	}
	return rows, contextMenuStart(selectedRow, len(rows), visible), visible
}

func inspectorFieldAt(panel rect, state ViewState, fields []inspectorField, y int) (int, bool) {
	rows, start, visible := inspectorFieldWindow(panel, state, fields)
	firstY := panel.Y + inspectorFieldListY
	if y < firstY || y >= firstY+visible {
		return 0, false
	}
	rowIndex := start + y - firstY
	if rowIndex < 0 || rowIndex >= len(rows) || rows[rowIndex].fieldIndex < 0 {
		return 0, false
	}
	return rows[rowIndex].fieldIndex, true
}

func inspectorFieldY(panel rect, state ViewState, fields []inspectorField, fieldIndex int) (int, bool) {
	rows, start, visible := inspectorFieldWindow(panel, state, fields)
	for rowIndex, row := range rows {
		if row.fieldIndex != fieldIndex || rowIndex < start || rowIndex >= start+visible {
			continue
		}
		return panel.Y + inspectorFieldListY + rowIndex - start, true
	}
	return 0, false
}
