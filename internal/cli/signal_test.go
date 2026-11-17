package cli

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

// Not parallel: the signals go to the whole test process.
func TestInterruptible(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			cancelled := false
			err := interruptible(func(ctx context.Context) error {
				if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
					t.Fatal(err)
				}
				select {
				case <-ctx.Done():
					cancelled = true
					return ctx.Err()
				case <-time.After(5 * time.Second):
					return errors.New("context was not cancelled")
				}
			})
			if !cancelled {
				t.Fatalf("work was not cancelled: %v", err)
			}
			ie, ok := errors.AsType[*InterruptedError](err)
			if !ok || ie.Signal != sig {
				t.Fatalf("err = %v, want InterruptedError{%v}", err, sig)
			}
			if got, want := ExitCode(err), 128+int(sig); got != want {
				t.Errorf("ExitCode = %d, want %d", got, want)
			}
		})
	}
}

func TestInterruptibleWithoutSignal(t *testing.T) {
	want := errors.New("work failed")
	if err := interruptible(func(context.Context) error { return want }); !errors.Is(err, want) {
		t.Errorf("err = %v, want the work's own error", err)
	}
	if err := interruptible(func(context.Context) error { return nil }); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}
