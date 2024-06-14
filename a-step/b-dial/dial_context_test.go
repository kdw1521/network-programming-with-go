package b_dial

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestDialContext(t *testing.T) {
	dl := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), dl)
	defer cancel()

	var d net.Dialer
	d.Control = func(_, _ string, _ syscall.RawConn) error {
		time.Sleep(5*time.Second + time.Millisecond)
		return nil
	}
	conn, err := d.DialContext(ctx, "tcp", "10.0.0.0:80")
	if err == nil {
		_ = conn.Close()
		t.Fatal("연결 성공!")
	}
	var nErr net.Error
	ok := errors.As(err, &nErr)
	if !ok {
		t.Error(err)
	} else {
		if !nErr.Timeout() {
			t.Errorf("타임 아웃 에러가 아님: %v", err)
		}
	}

	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Errorf("expected deadline exceeded; actual: %v", ctx.Err())
	}
}
