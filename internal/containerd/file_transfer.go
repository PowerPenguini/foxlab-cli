package containerd

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func (r *Runtime) PutFile(ctx context.Context, l *lab.Lab, ref workload.Ref, hostPath, guestPath string) error {
	if ref.Type != workload.TypeContainer {
		return fmt.Errorf("containerd cannot transfer files for workload type %q", ref.Type)
	}
	ct, ok := findContainer(l, ref.ID)
	if !ok {
		return fmt.Errorf("container not found: %s", ref.ID)
	}
	dir, name, err := splitGuestFilePath(guestPath)
	if err != nil {
		return err
	}
	if ct.Disk != "" {
		return putFileIntoMountedContainerRoot(l, ct, hostPath, guestPath)
	}
	file, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("host path is not a regular file: %s", hostPath)
	}
	var stderr bytes.Buffer
	err = r.execContainerTransfer(ctx, l, ct, []string{"/bin/sh", "-c", `exec cat > "$1/$2"`, "foxlab-cp", dir, name}, file, io.Discard, &stderr)
	if err != nil {
		return appendTransferStderr("put container file", err, stderr.String())
	}
	return nil
}

func (r *Runtime) GetFile(ctx context.Context, l *lab.Lab, ref workload.Ref, guestPath, hostPath string) error {
	if ref.Type != workload.TypeContainer {
		return fmt.Errorf("containerd cannot transfer files for workload type %q", ref.Type)
	}
	ct, ok := findContainer(l, ref.ID)
	if !ok {
		return fmt.Errorf("container not found: %s", ref.ID)
	}
	dir, name, err := splitGuestFilePath(guestPath)
	if err != nil {
		return err
	}
	if ct.Disk != "" {
		return getFileFromMountedContainerRoot(l, ct, guestPath, hostPath)
	}
	dest := hostPath
	if info, err := os.Stat(hostPath); err == nil && info.IsDir() {
		dest = filepath.Join(hostPath, filepath.Base(name))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	var stderr bytes.Buffer
	err = r.execContainerTransfer(ctx, l, ct, []string{"/bin/sh", "-c", `exec cat "$1/$2"`, "foxlab-cp", dir, name}, strings.NewReader(""), file, &stderr)
	if err != nil {
		return appendTransferStderr("get container file", err, stderr.String())
	}
	return nil
}

func putFileIntoMountedContainerRoot(l *lab.Lab, ct lab.Container, hostPath, guestPath string) error {
	dest, err := mountedContainerGuestPath(l, ct, guestPath)
	if err != nil {
		return err
	}
	src, err := os.Open(hostPath)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("host path is not a regular file: %s", hostPath)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func getFileFromMountedContainerRoot(l *lab.Lab, ct lab.Container, guestPath, hostPath string) error {
	srcPath, err := mountedContainerGuestPath(l, ct, guestPath)
	if err != nil {
		return err
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("guest path is not a regular file: %s", guestPath)
	}
	dest := hostPath
	if hostInfo, err := os.Stat(hostPath); err == nil && hostInfo.IsDir() {
		dest = filepath.Join(hostPath, filepath.Base(guestPath))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func mountedContainerGuestPath(l *lab.Lab, ct lab.Container, guestPath string) (string, error) {
	if _, _, err := splitGuestFilePath(guestPath); err != nil {
		return "", err
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return "", err
	}
	root := filepath.Join(mountPath, "merged")
	if info, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("container rootfs is not mounted: %w", err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("container rootfs is not a directory: %s", root)
	}
	rel := strings.TrimPrefix(path.Clean(guestPath), "/")
	dest := filepath.Clean(filepath.Join(root, rel))
	if dest != root && !strings.HasPrefix(dest, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("guest path escapes container rootfs: %s", guestPath)
	}
	return dest, nil
}

func (r *Runtime) execContainerTransfer(ctx context.Context, l *lab.Lab, ct lab.Container, args []string, in io.Reader, out, errOut io.Writer) error {
	client, cctx, closeClient, err := r.client(ctx)
	if err != nil {
		return err
	}
	defer closeClient()
	container, err := client.LoadContainer(cctx, l.ManagedContainerName(ct))
	if err != nil {
		return err
	}
	task, err := container.Task(cctx, nil)
	if err != nil {
		return err
	}
	taskStatus, err := task.Status(cctx)
	if err != nil {
		return err
	}
	if taskStatus.Status != containerd.Running {
		return fmt.Errorf("container task is %s, not running", taskStatus.Status)
	}
	spec, err := container.Spec(cctx)
	if err != nil {
		return err
	}
	execID := "foxlab-cp-" + strings.ToLower(ct.ID) + "-" + time.Now().Format("20060102150405.000000000")
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	process, err := task.Exec(cctx, execID, containerTransferProcess(args, spec.Process), cio.NewCreator(cio.WithStreams(in, out, errOut)))
	if err != nil {
		return err
	}
	if ioSet := process.IO(); ioSet != nil {
		defer ioSet.Cancel()
	}
	namespace := r.containerdNamespace()
	defer deleteShellProcess(namespace, process)
	statusC, err := process.Wait(cctx)
	if err != nil {
		return err
	}
	if err := process.Start(cctx); err != nil {
		return err
	}
	runCtx := namespaces.WithNamespace(ctx, namespace)
	var status containerd.ExitStatus
	select {
	case status = <-statusC:
	case <-runCtx.Done():
		deleteShellProcess(namespace, process)
		return runCtx.Err()
	}
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("container file transfer exited with status %d", code)
	}
	return nil
}

func containerTransferProcess(args []string, base *specs.Process) *specs.Process {
	process := specs.Process{}
	if base != nil {
		process = *base
	}
	process.Terminal = false
	if process.Cwd == "" {
		process.Cwd = "/"
	}
	process.Args = args
	return &process
}

func splitGuestFilePath(value string) (string, string, error) {
	value = path.Clean(strings.TrimSpace(value))
	if !path.IsAbs(value) {
		return "", "", fmt.Errorf("guest path must be absolute: %s", value)
	}
	name := path.Base(value)
	if name == "." || name == "/" || name == "" {
		return "", "", fmt.Errorf("guest path must include a file name: %s", value)
	}
	return path.Dir(value), name, nil
}

func tarHostFile(hostPath, guestName string) (io.Reader, <-chan error) {
	reader, writer := io.Pipe()
	errC := make(chan error, 1)
	go func() {
		errC <- writeTarHostFile(writer, hostPath, guestName)
	}()
	return reader, errC
}

func writeTarHostFile(writer *io.PipeWriter, hostPath, guestName string) error {
	file, err := os.Open(hostPath)
	if err != nil {
		_ = writer.CloseWithError(err)
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		_ = writer.CloseWithError(err)
		return err
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("host path is not a regular file: %s", hostPath)
		_ = writer.CloseWithError(err)
		return err
	}
	tw := tar.NewWriter(writer)
	header := &tar.Header{
		Name:    guestName,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		_ = writer.CloseWithError(err)
		return err
	}
	if _, err := io.Copy(tw, file); err != nil {
		_ = writer.CloseWithError(err)
		return err
	}
	if err := tw.Close(); err != nil {
		_ = writer.CloseWithError(err)
		return err
	}
	return writer.Close()
}

func extractSingleTarFile(reader io.Reader, hostPath string) error {
	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		return err
	}
	if header.FileInfo().IsDir() || !header.FileInfo().Mode().IsRegular() {
		return fmt.Errorf("guest path is not a regular file: %s", header.Name)
	}
	dest := hostPath
	if info, err := os.Stat(hostPath); err == nil && info.IsDir() {
		dest = filepath.Join(hostPath, filepath.Base(header.Name))
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode).Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, tr); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := tr.Next(); err != io.EOF {
		if err == nil {
			return errors.New("guest tar stream contained more than one file")
		}
		return err
	}
	return nil
}

func appendTransferStderr(operation string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w: %s", operation, err, stderr)
}
