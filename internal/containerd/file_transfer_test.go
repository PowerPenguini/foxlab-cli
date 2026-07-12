package containerd

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSingleTarFilePreservesDestinationWhenArchiveHasExtraEntry(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, entry := range []struct {
		name string
		data string
	}{{name: "first.txt", data: "replacement"}, {name: "second.txt", data: "unexpected"}} {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "dest.txt")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractSingleTarFile(&archive, dest); err == nil || !strings.Contains(err.Error(), "more than one file") {
		t.Fatalf("extractSingleTarFile error = %v, want multiple-file rejection", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("destination = %q, want original after rejected archive", data)
	}
}

func TestWriteAtomicHostFilePreservesDestinationOnFailure(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest.txt")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := writeAtomicHostFile(dest, 0o644, func(file *os.File) error {
		_, _ = file.WriteString("partial")
		return errors.New("transfer failed")
	})
	if err == nil {
		t.Fatal("expected transfer failure")
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("destination = %q", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".dest.txt.tmp-*")); len(matches) != 0 {
		t.Fatalf("temporary files remain: %#v", matches)
	}
}

func TestWriteAtomicHostFilePreservesExistingMode(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "dest.txt")
	if err := os.WriteFile(dest, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicHostFile(dest, 0o644, func(file *os.File) error {
		_, err := file.WriteString("replacement")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("destination mode = %o, want 600", got)
	}
}

func TestSplitGuestFilePathRequiresAbsoluteFile(t *testing.T) {
	dir, name, err := splitGuestFilePath("/tmp/file")
	if err != nil {
		t.Fatalf("splitGuestFilePath returned error: %v", err)
	}
	if dir != "/tmp" || name != "file" {
		t.Fatalf("splitGuestFilePath = %q %q, want /tmp file", dir, name)
	}
	if _, _, err := splitGuestFilePath("tmp/file"); err == nil {
		t.Fatal("expected relative guest path error")
	}
	if _, _, err := splitGuestFilePath("/"); err == nil {
		t.Fatal("expected missing file name error")
	}
}

func TestTarHostFileAndExtractSingleTarFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.txt")
	if err := os.WriteFile(src, []byte("hello\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	reader, errC := tarHostFile(src, "dest.txt")
	outDir := filepath.Join(dir, "out")
	if err := os.Mkdir(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractSingleTarFile(reader, outDir); err != nil {
		t.Fatalf("extractSingleTarFile returned error: %v", err)
	}
	if err := <-errC; err != nil {
		t.Fatalf("tarHostFile returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "dest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("extracted data = %q", data)
	}
}

func TestTarHostFileRejectsDirectories(t *testing.T) {
	reader, errC := tarHostFile(t.TempDir(), "dir")
	_, _ = io.Copy(io.Discard, reader)
	if err := <-errC; err == nil {
		t.Fatal("expected directory rejection")
	}
}

func TestContainerTransferProcessUsesNoTTY(t *testing.T) {
	process := containerTransferProcess([]string{"/bin/sh", "-c", "tar"}, nil)
	if process.Terminal {
		t.Fatal("transfer process uses a TTY")
	}
	if got := process.Args; len(got) != 3 || got[0] != "/bin/sh" || got[2] != "tar" {
		t.Fatalf("process args = %#v", got)
	}
}

func TestAppendTransferStderrIncludesConcreteStderr(t *testing.T) {
	err := appendTransferStderr("put container file", bytes.ErrTooLarge, "tar: permission denied\n")
	if err == nil || err.Error() != "put container file: bytes.Buffer: too large: tar: permission denied" {
		t.Fatalf("error = %v", err)
	}
}
