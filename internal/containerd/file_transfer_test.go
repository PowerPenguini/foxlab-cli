package containerd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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
