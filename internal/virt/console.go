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
	readCh    chan []byte
	errCh     chan error
	done      chan struct{}
	pending   []byte
	readMu    sync.Mutex
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func (r *LibvirtRuntime) OpenConsole(ctx context.Context, l *lab.Lab, id string) (*Console, error) {
	if err := ensureLibvirtEventLoop(); err != nil {
		return nil, err
	}
	path, err := r.consolePTY(ctx, l, id)
	if err != nil {
		return nil, err
	}
	dom, err := r.lookupRunningDomain(ctx, l, id)
	if err != nil {
		return nil, err
	}
	stream, err := r.conn.NewStream(libvirt.STREAM_NONBLOCK)
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
		readCh: make(chan []byte, 16),
		errCh:  make(chan error, 1),
		done:   make(chan struct{}),
	}
	if err := stream.EventAddCallback(libvirt.STREAM_EVENT_READABLE|libvirt.STREAM_EVENT_ERROR|libvirt.STREAM_EVENT_HANGUP, console.handleStreamEvent); err != nil {
		_ = stream.Abort()
		_ = stream.Free()
		_ = dom.Free()
		return nil, fmt.Errorf("watch console stream %q: %w", id, err)
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
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.pending) == 0 {
		select {
		case data := <-c.readCh:
			c.pending = data
		case err := <-c.errCh:
			if err == nil {
				err = io.EOF
			}
			return 0, err
		case <-c.done:
			return 0, io.ErrClosedPipe
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
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
			_ = c.stream.EventRemoveCallback()
			if err := c.stream.Abort(); err != nil {
				firstErr = err
			}
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

func (c *Console) handleStreamEvent(st *libvirt.Stream, events libvirt.StreamEventType) {
	if events&(libvirt.STREAM_EVENT_ERROR|libvirt.STREAM_EVENT_HANGUP) != 0 {
		c.reportReadError(io.EOF)
		return
	}
	if events&libvirt.STREAM_EVENT_READABLE == 0 {
		return
	}
	buf := make([]byte, 4096)
	for {
		n, err := st.Recv(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			select {
			case c.readCh <- data:
			case <-c.done:
				return
			}
		}
		if err != nil {
			if isTemporaryStreamError(err) {
				return
			}
			c.reportReadError(err)
			return
		}
		if n == 0 {
			return
		}
	}
}

func (c *Console) reportReadError(err error) {
	select {
	case c.errCh <- err:
	default:
	}
}

var (
	eventLoopOnce sync.Once
	eventLoopErr  error
)

func ensureLibvirtEventLoop() error {
	eventLoopOnce.Do(func() {
		eventLoopErr = libvirt.EventRegisterDefaultImpl()
		if eventLoopErr != nil {
			return
		}
		go func() {
			for {
				_ = libvirt.EventRunDefaultImpl()
				time.Sleep(time.Millisecond)
			}
		}()
	})
	return eventLoopErr
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
