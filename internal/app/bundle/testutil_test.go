package bundle_test

import (
	"context"
	"io"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeExecutor is a no-op test double for ports.RemoteExecutor.
//
// The bundle pipeline does not invoke any executor methods; the type exists
// solely to satisfy NewService's signature, which retains a
// ports.RemoteExecutor parameter for backwards compatibility with existing
// call sites that wire a nil or fake executor.
type fakeExecutor struct{}

func (f *fakeExecutor) Run(_ context.Context, _ string) (string, error) { return "", nil }

func (f *fakeExecutor) RunStream(_ context.Context, _ string, _, _ io.Writer) error { return nil }

func (f *fakeExecutor) Transfer(_ context.Context, _, _ string, _ bool) error { return nil }

func (f *fakeExecutor) TransferExcluding(_ context.Context, _, _ string, _ bool, _ []string) error {
	return nil
}

func (f *fakeExecutor) TransferFile(_ context.Context, _, _ string) error { return nil }

func (f *fakeExecutor) DryRunTransfer(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// fakeGenerator is a test double for ports.ConfigGenerator. It records whether
// Generate was called and returns a fixed error when err is set.
type fakeGenerator struct {
	err error
}

func (f *fakeGenerator) Generate(_ context.Context, _ ports.GeneratorInput, _ string) error {
	return f.err
}
