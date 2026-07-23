package packaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDHCPImageFinalStageIsScratchWithOnlyDnsmasq(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "images", "dhcp", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	finalStage := strings.LastIndex(dockerfile, "\nFROM ")
	if finalStage < 0 {
		t.Fatal("DHCP Dockerfile has no final stage")
	}
	final := dockerfile[finalStage+1:]
	for _, want := range []string{
		"FROM scratch",
		"COPY --from=build /src/src/dnsmasq /dnsmasq",
		`ENTRYPOINT ["/dnsmasq"]`,
	} {
		if !strings.Contains(final, want) {
			t.Fatalf("final DHCP image stage missing %q:\n%s", want, final)
		}
	}
	if strings.Contains(final, "RUN ") || strings.Contains(final, "apt-get") || strings.Contains(final, "apk ") {
		t.Fatalf("final DHCP image stage contains distribution tooling:\n%s", final)
	}
	if strings.Count(final, "\nCOPY ") != 1 {
		t.Fatalf("final DHCP image stage should copy only dnsmasq:\n%s", final)
	}
}

func TestDHCPImageBuildTargetImportsIntoFoxLabNamespace(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)
	for _, want := range []string{
		"DHCP_IMAGE ?= foxlab.local/dhcp:2.93",
		"dhcp-image:",
		`$(DOCKER) build --tag "$(DHCP_IMAGE)" images/dhcp`,
		`--network none --cap-add NET_ADMIN --cap-add NET_RAW`,
		`--interface=foxlab-check0`,
		`--namespace "$(CONTAINERD_NAMESPACE)" images import -`,
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("DHCP image Make target missing %q", want)
		}
	}
}
