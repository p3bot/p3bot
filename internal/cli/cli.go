// Package cli is p3bot's Cobra command tree. It parses arguments, calls the
// operational packages, and renders their results and errors. It holds no
// operational logic of its own.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/p3bot/p3bot/internal/fault"
)

// system is the process environment a run sees, captured once at the edge so
// tests can supply their own.
type system struct {
	stdout    io.Writer
	stderr    io.Writer
	stderrTTY bool
	getenv    func(string) string
	goos      string
	version   string
}

func osSystem() system {
	return system{
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		stderrTTY: term.IsTerminal(int(os.Stderr.Fd())),
		getenv:    os.Getenv,
		goos:      runtime.GOOS,
		version:   buildVersion(debug.ReadBuildInfo),
	}
}

// buildVersion reports the main module version Go recorded in the binary,
// because go install sets no linker flags.
func buildVersion(read func() (*debug.BuildInfo, bool)) string {
	info, ok := read()
	if !ok || info == nil || info.Main.Version == "" {
		return "(unknown)"
	}
	return info.Main.Version
}

// InterruptedError reports that a signal ended the run.
type InterruptedError struct {
	Signal syscall.Signal
}

func (e *InterruptedError) Error() string { return "interrupted by " + e.Signal.String() }

// Execute runs p3bot with args. Every error it returns has already been
// rendered, except an *InterruptedError.
func Execute(args []string) error {
	return interruptible(func(ctx context.Context) error {
		return run(ctx, args, osSystem())
	})
}

// interruptible runs work with a context cancelled by SIGINT or SIGTERM, and
// reports the signal in place of work's result when one arrived.
func interruptible(work func(context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	var caught atomic.Int32
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case sig := <-sigCh:
			caught.Store(int32(sig.(syscall.Signal)))
			// A second signal now takes the default action and ends the process.
			signal.Stop(sigCh)
			cancel()
		case <-done:
		}
	}()

	err := work(ctx)
	if s := caught.Load(); s != 0 {
		return &InterruptedError{Signal: syscall.Signal(s)}
	}
	return err
}

// ExitCode maps an error returned by Execute to the process exit code: 0 for
// nil, 128+signum for an interrupt, otherwise the fault code's exit.
func ExitCode(err error) int {
	if err == nil {
		return fault.ExitOK
	}
	if ie, ok := errors.AsType[*InterruptedError](err); ok {
		return 128 + int(ie.Signal)
	}
	return fault.From(err).Exit()
}

// run executes one invocation and renders any error it ends with, so every
// failure reaches the caller as text on stderr or one envelope on stdout.
func run(ctx context.Context, args []string, sys system) error {
	a := newApp(sys)
	return a.execute(ctx, a.newRoot(), args)
}

func (a *app) execute(ctx context.Context, root *cobra.Command, args []string) error {
	root.SetArgs(args)
	root.SetOut(a.sys.stdout)
	root.SetErr(a.sys.stderr)
	// Parsing can fail before flags are read, so decide --debug and --json from
	// the raw arguments until setup replaces them with the parsed values.
	a.setDebug(flagRequested(args, "debug"))
	err := root.ExecuteContext(ctx)
	if err == nil && a.stdout.err != nil {
		err = fmt.Errorf("writing to stdout: %w", a.stdout.err)
	}
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return err
	}
	fe := fault.From(err)
	if u, ok := errors.AsType[*usageError](err); ok {
		a.flags.json = a.flags.json || flagRequested(args, "json")
		fe = fault.New(fault.InvalidArgument, u.Error(), u.cmd+" --help").WithCause(u)
	}
	a.log.Debug("command failed", "code", fe.Code(), "exit", fe.Exit(), "error", err)
	a.renderError(fe)
	return fe
}
