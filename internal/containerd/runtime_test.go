package containerd

import (
	"testing"

	"foxlab-cli/internal/lab"
)

func TestContainerConfigHashIncludesShellAndImage(t *testing.T) {
	base := lab.Container{ID: "web", Image: "docker.io/library/bash:latest", Shell: "/usr/local/bin/bash", Command: []string{"/usr/local/bin/bash", "-lc", "sleep infinity"}}
	changedImage := base
	changedImage.Image = "docker.io/kalilinux/kali-rolling:latest"
	changedShell := base
	changedShell.Shell = "/bin/bash"

	if containerConfigHash(base) == containerConfigHash(changedImage) {
		t.Fatal("image change did not change hash")
	}
	if containerConfigHash(base) == containerConfigHash(changedShell) {
		t.Fatal("shell change did not change hash")
	}
}
