package virt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	libvirt "github.com/libvirt/libvirt-go"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

const qemuAgentFileChunkSize = 48 * 1024

type qemuAgentCommander interface {
	QemuAgentCommand(string, libvirt.DomainQemuAgentCommandTimeout, uint32) (string, error)
}

func (r *LibvirtRuntime) PutFile(ctx context.Context, l *lab.Lab, ref workload.Ref, hostPath, guestPath string) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot transfer files for workload type %q", ref.Type)
	}
	vm, ok := labVM(l, ref.ID)
	if !ok {
		return fmt.Errorf("vm not found: %s", ref.ID)
	}
	if _, _, err := splitGuestFilePath(guestPath); err != nil {
		return err
	}
	file, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("host path is not a regular file: %s", hostPath)
	}
	dom, err := r.runningDomain(ctx, l.ManagedDomainName(vm), vm.ID)
	if err != nil {
		return err
	}
	defer dom.Free()
	if err := qemuAgentPutFile(ctx, dom, file, guestPath); err != nil {
		return fmt.Errorf("put vm file %q: %w", vm.ID, err)
	}
	return nil
}

func (r *LibvirtRuntime) GetFile(ctx context.Context, l *lab.Lab, ref workload.Ref, guestPath, hostPath string) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot transfer files for workload type %q", ref.Type)
	}
	vm, ok := labVM(l, ref.ID)
	if !ok {
		return fmt.Errorf("vm not found: %s", ref.ID)
	}
	_, name, err := splitGuestFilePath(guestPath)
	if err != nil {
		return err
	}
	dest := hostPath
	if info, err := os.Stat(hostPath); err == nil && info.IsDir() {
		dest = filepath.Join(hostPath, filepath.Base(name))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	dom, err := r.runningDomain(ctx, l.ManagedDomainName(vm), vm.ID)
	if err != nil {
		return err
	}
	defer dom.Free()
	if err := qemuAgentGetFile(ctx, dom, guestPath, file); err != nil {
		return fmt.Errorf("get vm file %q: %w", vm.ID, err)
	}
	return nil
}

func (r *LibvirtRuntime) runningDomain(ctx context.Context, name, id string) (*libvirt.Domain, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dom, err := r.conn.LookupDomainByName(name)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("vm %s is missing; run it first", id)
		}
		return nil, err
	}
	state, _, err := dom.GetState()
	if err != nil {
		_ = dom.Free()
		return nil, err
	}
	if state != libvirt.DOMAIN_RUNNING {
		_ = dom.Free()
		return nil, fmt.Errorf("vm %s is %s; run it first", id, domainStateName(state))
	}
	return dom, nil
}

func qemuAgentPutFile(ctx context.Context, agent qemuAgentCommander, src io.Reader, guestPath string) error {
	handle, err := qemuAgentFileOpen(ctx, agent, guestPath, "w")
	if err != nil {
		return err
	}
	buf := make([]byte, qemuAgentFileChunkSize)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if err := qemuAgentFileWrite(ctx, agent, handle, buf[:n]); err != nil {
				return errors.Join(err, qemuAgentFileClose(context.Background(), agent, handle))
			}
		}
		if readErr == io.EOF {
			return qemuAgentFileClose(context.Background(), agent, handle)
		}
		if readErr != nil {
			return errors.Join(readErr, qemuAgentFileClose(context.Background(), agent, handle))
		}
	}
}

func qemuAgentGetFile(ctx context.Context, agent qemuAgentCommander, guestPath string, dst io.Writer) error {
	handle, err := qemuAgentFileOpen(ctx, agent, guestPath, "r")
	if err != nil {
		return err
	}
	for {
		chunk, eof, err := qemuAgentFileRead(ctx, agent, handle, qemuAgentFileChunkSize)
		if err != nil {
			return errors.Join(err, qemuAgentFileClose(context.Background(), agent, handle))
		}
		if len(chunk) > 0 {
			if _, err := dst.Write(chunk); err != nil {
				return errors.Join(err, qemuAgentFileClose(context.Background(), agent, handle))
			}
		}
		if eof {
			return qemuAgentFileClose(context.Background(), agent, handle)
		}
		if len(chunk) == 0 {
			return errors.Join(fmt.Errorf("guest agent returned an empty non-EOF read"), qemuAgentFileClose(context.Background(), agent, handle))
		}
	}
}

func qemuAgentFileOpen(ctx context.Context, agent qemuAgentCommander, guestPath, mode string) (int, error) {
	var handle int
	if err := qemuAgentExecute(ctx, agent, "guest-file-open", map[string]string{"path": guestPath, "mode": mode}, &handle); err != nil {
		return 0, err
	}
	return handle, nil
}

func qemuAgentFileClose(ctx context.Context, agent qemuAgentCommander, handle int) error {
	return qemuAgentExecute(ctx, agent, "guest-file-close", map[string]int{"handle": handle}, nil)
}

func qemuAgentFileWrite(ctx context.Context, agent qemuAgentCommander, handle int, data []byte) error {
	args := map[string]any{
		"handle":  handle,
		"buf-b64": base64.StdEncoding.EncodeToString(data),
	}
	var result struct {
		Count int `json:"count"`
	}
	if err := qemuAgentExecute(ctx, agent, "guest-file-write", args, &result); err != nil {
		return err
	}
	if result.Count != len(data) {
		return fmt.Errorf("guest agent wrote %d of %d bytes", result.Count, len(data))
	}
	return nil
}

func qemuAgentFileRead(ctx context.Context, agent qemuAgentCommander, handle, count int) ([]byte, bool, error) {
	args := map[string]int{"handle": handle, "count": count}
	var result struct {
		Count int    `json:"count"`
		Buf   string `json:"buf-b64"`
		EOF   bool   `json:"eof"`
	}
	if err := qemuAgentExecute(ctx, agent, "guest-file-read", args, &result); err != nil {
		return nil, false, err
	}
	data, err := base64.StdEncoding.DecodeString(result.Buf)
	if err != nil {
		return nil, false, fmt.Errorf("decode guest file chunk: %w", err)
	}
	if result.Count != len(data) {
		return nil, false, fmt.Errorf("guest agent returned %d bytes with %d decoded bytes", result.Count, len(data))
	}
	return data, result.EOF, nil
}

func qemuAgentExecute(ctx context.Context, agent qemuAgentCommander, execute string, args any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := map[string]any{"execute": execute}
	if args != nil {
		payload["arguments"] = args
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	response, err := agent.QemuAgentCommand(string(data), libvirt.DOMAIN_QEMU_AGENT_COMMAND_DEFAULT, 0)
	if err != nil {
		if qemuAgentUnavailable(err.Error()) {
			return fmt.Errorf("vm guest agent unavailable; install qemu-guest-agent and restart the VM: %w", err)
		}
		return err
	}
	var envelope struct {
		Return json.RawMessage `json:"return"`
		Error  *qemuAgentError `json:"error"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		return fmt.Errorf("decode guest agent response: %w", err)
	}
	if envelope.Error != nil {
		return fmt.Errorf("guest agent command %s failed: %s", execute, envelope.Error)
	}
	if out != nil {
		if len(envelope.Return) == 0 {
			return fmt.Errorf("guest agent command %s returned no payload", execute)
		}
		if err := json.Unmarshal(envelope.Return, out); err != nil {
			return fmt.Errorf("decode guest agent command %s result: %w", execute, err)
		}
	}
	return nil
}

type qemuAgentError struct {
	Class string `json:"class"`
	Desc  string `json:"desc"`
}

func (e qemuAgentError) Error() string {
	switch {
	case e.Class != "" && e.Desc != "":
		return e.Class + ": " + e.Desc
	case e.Desc != "":
		return e.Desc
	case e.Class != "":
		return e.Class
	default:
		return "unknown error"
	}
}

func qemuAgentUnavailable(detail string) bool {
	detail = strings.ToLower(detail)
	return strings.Contains(detail, "guest agent") ||
		strings.Contains(detail, "qemu-ga") ||
		strings.Contains(detail, "agent is not connected") ||
		strings.Contains(detail, "not responding")
}

func splitGuestFilePath(value string) (string, string, error) {
	value = path.Clean(strings.TrimSpace(value))
	if !path.IsAbs(value) {
		return "", "", fmt.Errorf("guest path must be absolute: %s", value)
	}
	name := path.Base(value)
	if name == "." || name == "/" || name == "" {
		return "", "", fmt.Errorf("guest path must include a file name: %s", value)
	}
	return path.Dir(value), name, nil
}
