package topologyui

import (
	"testing"

	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

func TestDiskCreateRequestPreservesDefaultsAliasesAndSizeSuffixes(t *testing.T) {
	request, err := diskCreateRequest("data", map[string]string{
		"size":   "12GB",
		"format": "RAW",
		"to":     "vm:vm1",
		"target": "container:web",
		"attach": "ct:legacy",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.ID != "data" || !request.SizeGB.Set || request.SizeGB.Value != 12 || request.Format != topology.DiskFormatRaw {
		t.Fatalf("disk create request = %#v", request)
	}
	if !request.AttachTo.Set || request.AttachTo.Value != (workload.Ref{Type: workload.TypeVM, ID: "vm1"}) {
		t.Fatalf("disk create target = %#v", request.AttachTo)
	}

	defaults, err := diskCreateRequest("default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.SizeGB.Set || defaults.Format != topology.DiskFormatQCOW2 || defaults.AttachTo.Set {
		t.Fatalf("disk create defaults = %#v", defaults)
	}

	emptySize, err := diskCreateRequest("empty-size", map[string]string{"size": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !emptySize.SizeGB.Set || emptySize.SizeGB.Value != defaultDiskCreateSizeGB {
		t.Fatalf("empty disk size request = %#v", emptySize)
	}
}

func TestDiskAttachAndDetachRequestsPreserveTargetSemantics(t *testing.T) {
	attach, err := diskAttachRequest("data", map[string]string{"to": "ct:web", "target": "vm:vm1"})
	if err != nil {
		t.Fatal(err)
	}
	if attach.DiskID != "data" || attach.Target != (workload.Ref{Type: workload.TypeContainer, ID: "web"}) {
		t.Fatalf("disk attach request = %#v", attach)
	}

	detach, err := diskDetachRequest("fallback", map[string]string{
		"type": "vm",
		"from": "ct:web",
		"disk": "data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if detach.Target != (workload.Ref{Type: workload.TypeContainer, ID: "web"}) || detach.DiskID != "data" {
		t.Fatalf("disk detach request = %#v", detach)
	}

	inferred, err := diskDetachRequest("vm1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if inferred.Target != (workload.Ref{ID: "vm1"}) {
		t.Fatalf("inferred disk detach request = %#v", inferred)
	}

	fallback, err := diskDetachRequest("vm1", map[string]string{"type": "vm", "from": "malformed"})
	if err != nil {
		t.Fatal(err)
	}
	if fallback.Target != (workload.Ref{Type: workload.TypeVM, ID: "vm1"}) {
		t.Fatalf("fallback disk detach request = %#v", fallback)
	}
}

func TestDiskResizeRequestParsesForceAliases(t *testing.T) {
	request, err := diskResizeRequest("data", map[string]string{"size": "12", "force": "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if request != (topology.DiskResizeRequest{DiskID: "data", SizeGB: 12, Force: true}) {
		t.Fatalf("disk resize request = %#v", request)
	}

	request, err = diskResizeRequest("data", map[string]string{"size": "8", "force": "off"})
	if err != nil {
		t.Fatal(err)
	}
	if request.Force {
		t.Fatalf("disk resize force = true, want false: %#v", request)
	}
}

func TestDiskRequestErrorsRemainStable(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "sorted unsupported create argument",
			run: func() error {
				_, err := diskCreateRequest("data", map[string]string{"zzz": "1", "siz": "2"})
				return err
			},
			want: "unsupported disk create argument: siz",
		},
		{
			name: "invalid create size",
			run: func() error {
				_, err := diskCreateRequest("data", map[string]string{"size": "abc"})
				return err
			},
			want: "invalid disk size: abc",
		},
		{
			name: "unsupported create format",
			run: func() error {
				_, err := diskCreateRequest("data", map[string]string{"format": "vmdk"})
				return err
			},
			want: "unsupported disk format: vmdk",
		},
		{
			name: "invalid create target",
			run: func() error {
				_, err := diskCreateRequest("data", map[string]string{"to": "pod:test"})
				return err
			},
			want: "usage: disk create <id> [size=N] [format=qcow2|raw] [to=vm:<id>|container:<id>]",
		},
		{
			name: "unsupported attach layer",
			run: func() error {
				_, err := diskAttachRequest("data", map[string]string{"to": "vm:vm1", "layer": "layer1"})
				return err
			},
			want: "unsupported disk attach argument: layer",
		},
		{
			name: "unsupported attach layerid",
			run: func() error {
				_, err := diskAttachRequest("data", map[string]string{"to": "vm:vm1", "layerid": "layer1"})
				return err
			},
			want: "unsupported disk attach argument: layerid",
		},
		{
			name: "missing attach target",
			run: func() error {
				_, err := diskAttachRequest("data", nil)
				return err
			},
			want: "usage: disk attach <id> to=vm:<id>|container:<id>",
		},
		{
			name: "unsupported detach argument",
			run: func() error {
				_, err := diskDetachRequest("vm1", map[string]string{"diskid": "data"})
				return err
			},
			want: "unsupported disk detach argument: diskid",
		},
		{
			name: "invalid detach type",
			run: func() error {
				_, err := diskDetachRequest("vm1", map[string]string{"type": "pod"})
				return err
			},
			want: "disk target must be vm or container",
		},
		{
			name: "unsupported resize argument",
			run: func() error {
				_, err := diskResizeRequest("data", map[string]string{"siz": "12"})
				return err
			},
			want: "unsupported disk resize argument: siz",
		},
		{
			name: "invalid resize size",
			run: func() error {
				_, err := diskResizeRequest("data", map[string]string{"size": "abc"})
				return err
			},
			want: "usage: disk resize <id> size=N [force=true]",
		},
		{
			name: "invalid resize force",
			run: func() error {
				_, err := diskResizeRequest("data", map[string]string{"size": "8", "force": "maybe"})
				return err
			},
			want: "invalid disk resize force: maybe",
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
