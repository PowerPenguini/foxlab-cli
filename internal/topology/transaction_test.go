package topology

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestMutateLabRollsBackCallbackFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}}}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	want := lab.Clone(service.Lab)
	sentinel := errors.New("abort mutation")
	err = service.mutateLab(func(current *lab.Lab) error {
		current.VMs[0].CPUs = 99
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("mutateLab error = %v, want sentinel", err)
	}
	if !reflect.DeepEqual(service.Lab, want) {
		t.Fatalf("callback failure changed in-memory lab:\ngot  %#v\nwant %#v", service.Lab, want)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, want) {
		t.Fatalf("callback failure changed saved lab:\ngot  %#v\nwant %#v", reloaded, want)
	}
}

func TestMutateLabRollsBackSaveFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}}}
	service := NewService(lab.Clone(initial), filepath.Join(blocker, "demo.lab"))
	err := service.mutateLab(func(current *lab.Lab) error {
		current.VMs[0].CPUs = 99
		return nil
	})
	if err == nil {
		t.Fatal("mutateLab unexpectedly succeeded")
	}
	if !reflect.DeepEqual(service.Lab, initial) {
		t.Fatalf("save failure changed in-memory lab:\ngot  %#v\nwant %#v", service.Lab, initial)
	}
}

func TestMutateLabCommitsAndRefreshesFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}}}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	service := NewService(lab.Clone(initial), path)
	before := service.Lab
	if err := service.mutateLab(func(current *lab.Lab) error {
		current.VMs[0].CPUs = 4
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if service.Lab == before {
		t.Fatal("successful mutation did not refresh the in-memory lab")
	}
	if got := service.Lab.VMs[0].CPUs; got != 4 {
		t.Fatalf("in-memory CPUs = %d, want 4", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, service.Lab) {
		t.Fatalf("saved lab differs from refreshed lab:\ngot  %#v\nwant %#v", reloaded, service.Lab)
	}
}
