package virt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	libvirt "github.com/libvirt/libvirt-go"
)

type fakeQemuAgent struct {
	writes bytes.Buffer
	reads  [][]byte
	calls  []string
}

func (f *fakeQemuAgent) QemuAgentCommand(command string, _ libvirt.DomainQemuAgentCommandTimeout, _ uint32) (string, error) {
	var payload struct {
		Execute   string          `json:"execute"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(command), &payload); err != nil {
		return "", err
	}
	f.calls = append(f.calls, payload.Execute)
	switch payload.Execute {
	case "guest-file-open":
		return `{"return":7}`, nil
	case "guest-file-write":
		var args struct {
			Buf string `json:"buf-b64"`
		}
		if err := json.Unmarshal(payload.Arguments, &args); err != nil {
			return "", err
		}
		data, err := base64.StdEncoding.DecodeString(args.Buf)
		if err != nil {
			return "", err
		}
		_, _ = f.writes.Write(data)
		return fmt.Sprintf(`{"return":{"count":%d}}`, len(data)), nil
	case "guest-file-read":
		if len(f.reads) == 0 {
			return `{"return":{"count":0,"buf-b64":"","eof":true}}`, nil
		}
		data := f.reads[0]
		f.reads = f.reads[1:]
		return fmt.Sprintf(`{"return":{"count":%d,"buf-b64":%q,"eof":%t}}`, len(data), base64.StdEncoding.EncodeToString(data), len(f.reads) == 0), nil
	case "guest-file-close":
		return `{"return":{}}`, nil
	default:
		return `{"error":{"class":"CommandNotFound","desc":"unknown"}}`, nil
	}
}

func TestQemuAgentPutFileChunksData(t *testing.T) {
	agent := &fakeQemuAgent{}
	if err := qemuAgentPutFile(context.Background(), agent, strings.NewReader("hello"), "/tmp/file"); err != nil {
		t.Fatalf("qemuAgentPutFile returned error: %v", err)
	}
	if agent.writes.String() != "hello" {
		t.Fatalf("written data = %q", agent.writes.String())
	}
	want := []string{"guest-file-open", "guest-file-write", "guest-file-close"}
	if strings.Join(agent.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %#v, want %#v", agent.calls, want)
	}
}

func TestQemuAgentGetFileReadsUntilEOF(t *testing.T) {
	agent := &fakeQemuAgent{reads: [][]byte{[]byte("hel"), []byte("lo")}}
	var out bytes.Buffer
	if err := qemuAgentGetFile(context.Background(), agent, "/tmp/file", &out); err != nil {
		t.Fatalf("qemuAgentGetFile returned error: %v", err)
	}
	if out.String() != "hello" {
		t.Fatalf("read data = %q", out.String())
	}
	want := []string{"guest-file-open", "guest-file-read", "guest-file-read", "guest-file-close"}
	if strings.Join(agent.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %#v, want %#v", agent.calls, want)
	}
}

func TestQemuAgentExecuteReportsAgentUnavailable(t *testing.T) {
	agent := unavailableAgent{}
	err := qemuAgentExecute(context.Background(), agent, "guest-file-open", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "vm guest agent unavailable; install qemu-guest-agent and restart the VM") {
		t.Fatalf("error = %v, want unavailable guest agent guidance", err)
	}
}

type unavailableAgent struct{}

func (unavailableAgent) QemuAgentCommand(string, libvirt.DomainQemuAgentCommandTimeout, uint32) (string, error) {
	return "", fmt.Errorf("guest agent is not responding")
}
