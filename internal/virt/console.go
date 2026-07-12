package virt

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	libvirt "github.com/libvirt/libvirt-go"

	"foxlab-cli/internal/lab"
)

type Console struct {
	domain    *libvirt.Domain
	stream    *libvirt.Stream
	path      string
	recv      func([]byte) (int, error)
	done      chan struct{}
	readMu    sync.Mutex
	recvMu    sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func (r *LibvirtRuntime) OpenConsole(ctx context.Context, l *lab.Lab, id string) (*Console, error) {
	path, err := r.consolePTY(ctx, l, id)
	if err != nil {
		return nil, err
	}
	dom, err := r.lookupRunningDomain(ctx, l, id)
	if err != nil {
		return nil, err
	}
	stream, err := r.conn.NewStream(0)
	if err != nil {
		_ = dom.Free()
		return nil, fmt.Errorf("create console stream %q: %w", id, err)
	}
	if err := dom.OpenConsole("", stream, libvirt.DOMAIN_CONSOLE_FORCE); err != nil {
		_ = stream.Free()
		_ = dom.Free()
		return nil, fmt.Errorf("open domain console %q: %w", id, err)
	}
	console := &Console{
		domain: dom,
		stream: stream,
		path:   path,
		recv:   stream.Recv,
		done:   make(chan struct{}),
	}
	return console, nil
}

func (r *LibvirtRuntime) consolePTY(ctx context.Context, l *lab.Lab, id string) (string, error) {
	dom, err := r.lookupRunningDomain(ctx, l, id)
	if err != nil {
		return "", err
	}
	defer dom.Free()
	xmlText, err := dom.GetXMLDesc(0)
	if err != nil {
		return "", fmt.Errorf("get domain xml %q: %w", id, err)
	}
	path, ok := consolePTYFromDomainXML(xmlText)
	if !ok {
		return "", fmt.Errorf("domain %q has no pty console", id)
	}
	return path, nil
}

func (r *LibvirtRuntime) lookupRunningDomain(ctx context.Context, l *lab.Lab, id string) (*libvirt.Domain, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	vm, ok := labVM(l, id)
	if !ok {
		return nil, fmt.Errorf("vm not found: %s", id)
	}
	dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
	if err != nil {
		return nil, fmt.Errorf("lookup domain %q: %w", id, err)
	}
	state, _, err := dom.GetState()
	if err != nil {
		_ = dom.Free()
		return nil, fmt.Errorf("get domain state %q: %w", id, err)
	}
	if state != libvirt.DOMAIN_RUNNING {
		_ = dom.Free()
		return nil, fmt.Errorf("domain %q is not running", id)
	}
	return dom, nil
}

func (c *Console) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

func (c *Console) Read(p []byte) (int, error) {
	if c == nil {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		n, err := c.recvStream(p)
		if n > 0 {
			return n, nil
		}
		if err != nil {
			if isTemporaryStreamError(err) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return 0, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *Console) recvStream(p []byte) (int, error) {
	if c == nil || c.recv == nil {
		return 0, io.ErrClosedPipe
	}
	select {
	case <-c.done:
		return 0, io.ErrClosedPipe
	default:
	}
	c.recvMu.Lock()
	defer c.recvMu.Unlock()
	if c.recv == nil {
		return 0, io.ErrClosedPipe
	}
	return c.recv(p)
}

func (c *Console) Write(p []byte) (int, error) {
	if c == nil || c.stream == nil {
		return 0, io.ErrClosedPipe
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	written := 0
	for len(p) > 0 {
		select {
		case <-c.done:
			return written, io.ErrClosedPipe
		default:
		}
		n, err := c.stream.Send(p)
		if n > 0 {
			written += n
			p = p[n:]
		}
		if err != nil {
			if isTemporaryStreamError(err) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if written > 0 {
				return written, nil
			}
			return 0, err
		}
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return written, nil
}

func (c *Console) Close() error {
	if c == nil {
		return nil
	}
	var firstErr error
	c.closeOnce.Do(func() {
		close(c.done)
		if c.stream != nil {
			if err := c.stream.Abort(); err != nil {
				firstErr = err
			}
			c.recvMu.Lock()
			c.recv = nil
			c.recvMu.Unlock()
			if err := c.stream.Free(); err != nil && firstErr == nil {
				firstErr = err
			}
			c.stream = nil
		}
		if c.domain != nil {
			if err := c.domain.Free(); err != nil && firstErr == nil {
				firstErr = err
			}
			c.domain = nil
		}
	})
	return firstErr
}

func isTemporaryStreamError(err error) bool {
	if err == nil {
		return false
	}
	if libvirtErr, ok := err.(libvirt.Error); ok {
		if libvirtErr.Code == 0 && libvirtErr.Message == "" {
			return true
		}
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "temporarily unavailable") ||
		strings.Contains(text, "would block") ||
		strings.Contains(text, "again")
}

func consolePTYFromDomainXML(xmlText string) (string, bool) {
	var domain struct {
		Devices struct {
			Consoles []struct {
				Type   string `xml:"type,attr"`
				TTY    string `xml:"tty,attr"`
				Source struct {
					Path string `xml:"path,attr"`
				} `xml:"source"`
			} `xml:"console"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(xmlText), &domain); err != nil {
		return "", false
	}
	for _, console := range domain.Devices.Consoles {
		if console.Type != "pty" {
			continue
		}
		if console.Source.Path != "" {
			return console.Source.Path, true
		}
		if console.TTY != "" {
			return console.TTY, true
		}
	}
	return "", false
}
