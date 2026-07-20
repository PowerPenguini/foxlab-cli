package topologyui

import (
	"reflect"
	"testing"
)

func TestVMCreateRequestPreservesAliasesAndPrecedence(t *testing.T) {
	request, err := vmCreateRequest("fallback", map[string]string{
		"name":     "vm1",
		"cpus":     "4",
		"memory":   "2048",
		"mem":      "4096",
		"disk":     "disks/vm1.qcow2",
		"switch":   "lan",
		"external": "old-uplink",
		"uplink":   "wan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Name != "vm1" || !request.CPUs.Set || request.CPUs.Value != 4 || !request.MemoryMB.Set || request.MemoryMB.Value != 4096 {
		t.Fatalf("vm create request = %#v", request)
	}
	if request.Disk != "disks/vm1.qcow2" || request.Network.Switch != "lan" || request.Network.Uplink != "wan" {
		t.Fatalf("vm create request = %#v", request)
	}
}

func TestVMUpdateRequestPreservesExplicitClears(t *testing.T) {
	update, err := vmUpdateRequest(map[string]string{"disk": "", "iso": "", "vnc": "off"})
	if err != nil {
		t.Fatal(err)
	}
	if !update.Disk.Set || update.Disk.Value != "" || !update.ISO.Set || update.ISO.Value != "" {
		t.Fatalf("vm clear update = %#v", update)
	}
	if !update.VNC.Set || update.VNC.Value {
		t.Fatalf("vm vnc update = %#v", update.VNC)
	}
}

func TestContainerRequestsPreserveCommandEnvAndClears(t *testing.T) {
	request, err := containerCreateRequest("web", map[string]string{
		"image":   "nginx",
		"command": "nginx -g daemon-off",
		"shell":   "/bin/bash",
		"env":     "FOO=bar, BAZ=qux",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.Command, []string{"nginx", "-g", "daemon-off"}) {
		t.Fatalf("container command = %#v", request.Command)
	}
	if !reflect.DeepEqual(request.Env, map[string]string{"FOO": "bar", "BAZ": "qux"}) {
		t.Fatalf("container env = %#v", request.Env)
	}
	if request.Shell != "/bin/bash" {
		t.Fatalf("container shell = %q", request.Shell)
	}

	update, err := containerUpdateRequest(map[string]string{"command": "", "shell": "", "env": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !update.Command.Set || len(update.Command.Value) != 0 || !update.Shell.Set || update.Shell.Value != "" || !update.Env.Set || len(update.Env.Value) != 0 {
		t.Fatalf("container clear update = %#v", update)
	}
}

func TestWorkloadRequestErrorsRemainStable(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "sorted unsupported vm create argument",
			run: func() error {
				_, err := vmCreateRequest("vm1", map[string]string{"zzz": "1", "aaa": "2"})
				return err
			},
			want: "unsupported vm create argument: aaa",
		},
		{
			name: "invalid vm cpus",
			run: func() error {
				_, err := vmCreateRequest("vm1", map[string]string{"cpus": "zero"})
				return err
			},
			want: "invalid vm cpus: zero",
		},
		{
			name: "invalid vm memory alias",
			run: func() error {
				_, err := vmUpdateRequest(map[string]string{"mem": "-1"})
				return err
			},
			want: "invalid vm memory: -1",
		},
		{
			name: "invalid vm vnc",
			run: func() error {
				_, err := vmUpdateRequest(map[string]string{"vnc": "maybe"})
				return err
			},
			want: "invalid vm vnc: maybe",
		},
		{
			name: "unsupported container set argument",
			run: func() error {
				_, err := containerUpdateRequest(map[string]string{"unknown": "value"})
				return err
			},
			want: "unsupported container set argument: unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
