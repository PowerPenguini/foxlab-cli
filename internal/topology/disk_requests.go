package topology

import "foxlab-cli/internal/workload"

type DiskFormat string

const (
	DiskFormatQCOW2 DiskFormat = "qcow2"
	DiskFormatRaw   DiskFormat = "raw"
)

type DiskCreateRequest struct {
	ID       string
	SizeGB   Field[int]
	Format   DiskFormat
	AttachTo Field[workload.Ref]
}

type DiskAttachRequest struct {
	DiskID string
	Target workload.Ref
}

type DiskDetachRequest struct {
	Target workload.Ref
	DiskID string
}

type DiskResizeRequest struct {
	DiskID string
	SizeGB int
	Force  bool
}
