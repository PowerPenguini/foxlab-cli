package containerd

import (
	"errors"
	"strings"
	"testing"
)

func TestWithAccessHintAddsContainerdPermissionHint(t *testing.T) {
	err := WithAccessHint(errors.New(`failed to dial "/run/containerd/containerd.sock": permission denied`))
	if err == nil {
		t.Fatal("WithAccessHint returned nil")
	}
	for _, want := range []string{
		"permission denied",
		"run with sudo",
		"containerd socket",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestWithAccessHintSkipsUnrelatedErrors(t *testing.T) {
	original := errors.New("permission denied")
	if got := WithAccessHint(original); got != original {
		t.Fatalf("WithAccessHint changed unrelated error: %v", got)
	}
}

func TestWithAccessHintDoesNotDuplicateHint(t *testing.T) {
	original := errors.New(`failed to dial "/run/containerd/containerd.sock": permission denied; run with sudo or grant access to the containerd socket`)
	got := WithAccessHint(original)
	if strings.Count(got.Error(), "run with sudo") != 1 {
		t.Fatalf("hint duplicated: %q", got.Error())
	}
}
