package macnat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	DefaultDevicePath = "/dev/macnat"
	ModuleName        = "foxlab_macnat"
)

type Controller struct {
	DevicePath string
}

type Session struct {
	LabID    string
	SwitchID string
	Bridge   string
	Uplink   string
	MACs     []string
}

func NewController(devicePath string) Controller {
	if devicePath == "" {
		devicePath = DefaultDevicePath
	}
	return Controller{DevicePath: devicePath}
}

func (c Controller) Available() error {
	if _, err := os.Stat(c.devicePath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s device %s is missing; build and load the kernel module before using macnat uplinks", ModuleName, c.devicePath())
		}
		return err
	}
	return nil
}

func (c Controller) Configure(ctx context.Context, sessions []Session) error {
	if len(sessions) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Available(); err != nil {
		return err
	}
	commands, err := configureCommands(sessions)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err := c.writeCommand(ctx, command); err != nil {
			return err
		}
	}
	return nil
}

func (c Controller) Clear(ctx context.Context, labID string) error {
	if strings.TrimSpace(labID) == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(c.devicePath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	command, err := clearCommand(labID)
	if err != nil {
		return err
	}
	return c.writeCommand(ctx, command)
}

func configureCommands(sessions []Session) ([][]byte, error) {
	labID := sessions[0].LabID
	clear, err := clearCommand(labID)
	if err != nil {
		return nil, err
	}
	commands := [][]byte{clear}
	for _, session := range sessions {
		if session.LabID != labID {
			return nil, fmt.Errorf("macnat configure cannot mix lab IDs %q and %q", labID, session.LabID)
		}
		command, err := configureCommand(session)
		if err != nil {
			return nil, err
		}
		commands = append(commands, command)
	}
	return commands, nil
}

func configureCommand(session Session) ([]byte, error) {
	if err := validateSession(session); err != nil {
		return nil, err
	}
	values := []string{session.LabID, session.SwitchID, session.Bridge, session.Uplink}
	for _, value := range values {
		if strings.ContainsAny(value, " \t\r\n") {
			return nil, fmt.Errorf("macnat command value %q contains whitespace", value)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "configure labID=%s switchID=%s bridge=%s uplink=%s", session.LabID, session.SwitchID, session.Bridge, session.Uplink)
	for _, macText := range session.MACs {
		mac, err := net.ParseMAC(macText)
		if err != nil {
			return nil, fmt.Errorf("macnat MAC %q is invalid: %w", macText, err)
		}
		fmt.Fprintf(&b, " mac=%s", mac.String())
	}
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

func validateSession(session Session) error {
	if session.LabID == "" || session.SwitchID == "" || session.Bridge == "" || session.Uplink == "" {
		return fmt.Errorf("macnat session needs labID, switchID, bridge and uplink")
	}
	if len(session.MACs) == 0 {
		return fmt.Errorf("macnat session %q needs at least one workload MAC", session.SwitchID)
	}
	return nil
}

func clearCommand(labID string) ([]byte, error) {
	if strings.ContainsAny(labID, " \t\r\n") {
		return nil, fmt.Errorf("macnat command value %q contains whitespace", labID)
	}
	return []byte(fmt.Sprintf("clear labID=%s\n", labID)), nil
}

func (c Controller) writeCommand(ctx context.Context, data []byte) error {
	f, err := os.OpenFile(c.devicePath(), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := f.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("short write to %s: wrote %d of %d bytes", c.devicePath(), n, len(data))
	}
	return nil
}

func (c Controller) devicePath() string {
	if c.DevicePath == "" {
		return DefaultDevicePath
	}
	return c.DevicePath
}
