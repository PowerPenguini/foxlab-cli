package lab

import "fmt"

func validateDisks(l *Lab, index validationIndex) []string {
	var problems []string
	vmIDs := index.vmIDs
	containerIDs := index.containerIDs
	vmNames := index.vmNames
	containerNames := index.containerNames
	diskIDs := map[string]struct{}{}
	diskKinds := map[string]string{}
	for _, disk := range l.Disks {
		if !validID(disk.ID) {
			problems = append(problems, fmt.Sprintf("disk %q has invalid id", disk.ID))
		}
		if _, exists := diskIDs[disk.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate disk id %q", disk.ID))
		}
		diskIDs[disk.ID] = struct{}{}
		diskKinds[disk.ID] = normalizedDiskKind(disk)
	}
	for _, disk := range l.Disks {
		if disk.Path == "" {
			problems = append(problems, fmt.Sprintf("disk %q path is required", disk.ID))
		}
		if disk.SizeGB < 0 {
			problems = append(problems, fmt.Sprintf("disk %q sizeGB must not be negative", disk.ID))
		}
		if disk.Format != "" && disk.Format != "qcow2" && disk.Format != "raw" {
			problems = append(problems, fmt.Sprintf("disk %q format must be qcow2 or raw", disk.ID))
		}
		if disk.Kind != "" && disk.Kind != "base" && disk.Kind != "layer" && disk.Kind != "data" {
			problems = append(problems, fmt.Sprintf("disk %q kind must be base, layer or data", disk.ID))
		}
		kind := normalizedDiskKind(disk)
		if kind == "layer" && disk.Base == "" {
			problems = append(problems, fmt.Sprintf("disk %q layer requires base", disk.ID))
		}
		if kind == "data" && disk.Base != "" {
			problems = append(problems, fmt.Sprintf("disk %q data disk must not reference base", disk.ID))
		}
		if kind == "base" && disk.Base != "" {
			problems = append(problems, fmt.Sprintf("disk %q base disk must not reference base", disk.ID))
		}
		if disk.Base != "" {
			baseKind, baseExists := diskKinds[disk.Base]
			switch {
			case disk.Base == disk.ID:
				problems = append(problems, fmt.Sprintf("disk %q must not use itself as base", disk.ID))
			case !baseExists:
				problems = append(problems, fmt.Sprintf("disk %q references missing base disk %q", disk.ID, disk.Base))
			case baseKind != "base":
				problems = append(problems, fmt.Sprintf("disk %q base disk %q must be a base disk", disk.ID, disk.Base))
			}
		}
		if disk.AttachedType == "" && disk.AttachedTo != "" {
			problems = append(problems, fmt.Sprintf("disk %q attachedTo requires attachedType", disk.ID))
		}
		switch disk.AttachedType {
		case "":
		case "vm":
			if _, ok := vmIDs[disk.AttachedTo]; !ok {
				problems = append(problems, fmt.Sprintf("disk %q references missing vm %q", disk.ID, disk.AttachedTo))
			}
			if disk.Kind == "data" {
				problems = append(problems, fmt.Sprintf("disk %q data disk cannot attach to vm", disk.ID))
			}
		case "container":
			if _, ok := containerIDs[disk.AttachedTo]; !ok {
				problems = append(problems, fmt.Sprintf("disk %q references missing container %q", disk.ID, disk.AttachedTo))
			}
		default:
			problems = append(problems, fmt.Sprintf("disk %q attachedType must be vm or container", disk.ID))
		}
	}
	attachedDisks := map[string]string{}
	for _, disk := range l.Disks {
		if disk.AttachedType == "" || disk.AttachedTo == "" {
			continue
		}
		key := disk.AttachedType + ":" + disk.AttachedTo
		displayKey := workloadDisplayRef(disk.AttachedType, disk.AttachedTo, vmNames, containerNames)
		if existing, exists := attachedDisks[key]; exists {
			problems = append(problems, fmt.Sprintf("disks %q and %q are both attached to %s", existing, disk.ID, displayKey))
			continue
		}
		attachedDisks[key] = disk.ID
		if workloadDisk := l.attachedWorkloadDiskPath(disk.AttachedType, disk.AttachedTo); workloadDisk == "" {
			problems = append(problems, fmt.Sprintf("disk %q is attached to %s but workload disk is empty", disk.ID, displayKey))
		} else if l.ResolvePath(workloadDisk) != l.ResolvePath(disk.Path) {
			problems = append(problems, fmt.Sprintf("disk %q attachment path does not match %s disk", disk.ID, displayKey))
		}
	}
	return problems
}
