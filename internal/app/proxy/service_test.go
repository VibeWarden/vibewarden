package proxy

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

// fakeServer is a simple in-memory implementation of ports.ProxyServer for testing.
type fakeServer struct {
	startErr  error
	stopErr   error
	reloadErr error
	// startBlocks controls whether Start blocks on ctx or returns immediately.
	startBlocks bool
	// stopped tracks whether Stop was called.
	stopped bool
	// reloaded tracks whether Reload was called.
	reloaded bool
}

func (f *fakeServer) Start(ctx context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	if f.startBlocks {
		<-ctx.Done()
		return nil
	}
	return nil
}

func (f *fakeServer) Stop(_ context.Context) error {
	f.stopped = true
	return f.stopErr
}

func (f *fakeServer) Reload(_ context.Context) error {
	f.reloaded = true
	return f.reloadErr
}

func TestService_Run_ContextCancellation(t *testing.T) {
	server := &fakeServer{startBlocks: true}
	svc := NewService(server, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	// Give the service a moment to start.
	time.Sleep(10 * time.Millisecond)

	// Cancel the context to trigger graceful shutdown.
	cancel()

	select {
	case err := <-errCh:
		// context.Canceled is expected on graceful shutdown.
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Run() error = %v, want context.Canceled or nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Run() did not return after context cancellation")
	}

	if !server.stopped {
		t.Error("Stop() was not called after context cancellation")
	}
}

func TestService_Run_ServerError(t *testing.T) {
	want := errors.New("listen tcp: address in use")
	server := &fakeServer{startErr: want}
	svc := NewService(server, slog.Default())

	err := svc.Run(context.Background())
	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !errors.Is(err, want) {
		t.Errorf("Run() error = %v, want wrapping %v", err, want)
	}
}

func TestService_Run_StopError(t *testing.T) {
	stopErr := errors.New("stop failed")
	server := &fakeServer{
		startBlocks: true,
		stopErr:     stopErr,
	}
	svc := NewService(server, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, stopErr) {
			t.Errorf("Run() error = %v, want wrapping %v", err, stopErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("Run() did not return after context cancellation")
	}
}

func TestService_Reload(t *testing.T) {
	tests := []struct {
		name      string
		reloadErr error
		wantErr   bool
	}{
		{
			name:      "successful reload",
			reloadErr: nil,
			wantErr:   false,
		},
		{
			name:      "reload error propagates",
			reloadErr: errors.New("config invalid"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &fakeServer{reloadErr: tt.reloadErr}
			svc := NewService(server, slog.Default())

			err := svc.Reload(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Reload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !server.reloaded {
				t.Error("Reload() did not call server.Reload()")
			}
		})
	}
}

func TestNewService(t *testing.T) {
	server := &fakeServer{}
	logger := slog.Default()

	svc := NewService(server, logger)

	if svc == nil {
		t.Fatal("NewService() returned nil")
	}
	if svc.server != server {
		t.Error("NewService() did not set server correctly")
	}
	if svc.logger != logger {
		t.Error("NewService() did not set logger correctly")
	}
}
