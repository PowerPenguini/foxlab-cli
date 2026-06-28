package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/foxruntime"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/reconciler"
	"foxlab-cli/internal/virt"
	"foxlab-cli/internal/workload"
)

func main() {
	fs := flag.NewFlagSet("foxlabd", flag.ExitOnError)
	labPath := fs.String("lab", "", ".lab file to reconcile")
	interval := fs.Duration("interval", time.Second, "reconcile interval")
	uri := fs.String("uri", virt.DefaultURI, "libvirt URI")
	containerdAddress := fs.String("containerd", "", "containerd socket path")
	statusSocket := fs.String("status-socket", "", "Unix socket path for status YAML queries")
	once := fs.Bool("once", false, "run one reconcile step and exit")
	destroy := fs.Bool("destroy", false, "destroy workloads and managed host resources for the lab and exit")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: foxlabd [--lab demo.lab] [--interval 1s] [--once]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	resolvedLabPath, err := resolveLabPath(*labPath, fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	*labPath = resolvedLabPath
	if *labPath == "" {
		path, ok, err := lab.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if ok {
			*labPath = path
		}
	}
	if *destroy {
		loaded, err := lab.LoadFile(*labPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := foxruntime.DestroyLab(context.Background(), *uri, *containerdAddress, loaded); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	statusStore := daemonstatus.NewStore()
	var statusErrs <-chan error
	if !*once {
		path, errs, err := daemonstatus.Start(ctx, *statusSocket, statusStore)
		if err != nil {
			fmt.Fprintln(os.Stderr, "status socket:", err)
			os.Exit(1)
		}
		logger.Printf("status socket %s", path)
		statusErrs = errs
	}
	runner := reconciler.Runner{
		LabPath:     *labPath,
		Interval:    *interval,
		Logger:      logger,
		StatusStore: statusStore,
		RuntimeFactory: func(l *lab.Lab) (workload.Runtime, error) {
			return foxruntime.New(*uri, *containerdAddress, l)
		},
	}
	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx, *once)
	}()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case err := <-statusErrs:
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "status socket:", err)
			os.Exit(1)
		}
	}
}

func resolveLabPath(flagPath string, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("unexpected argument %q", args[1])
	}
	if flagPath != "" && len(args) > 0 {
		return "", fmt.Errorf("unexpected argument %q; --lab is already set", args[0])
	}
	if flagPath == "" && len(args) == 1 {
		return args[0], nil
	}
	return flagPath, nil
}
