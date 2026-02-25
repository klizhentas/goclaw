package listener

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestCLIListener_Run_ExitsOnContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	l := NewCLIListener(pr, &bytes.Buffer{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- l.Run(ctx, func(context.Context, InboundMessage) error { return nil })
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("listener did not exit on canceled context")
	}
}
