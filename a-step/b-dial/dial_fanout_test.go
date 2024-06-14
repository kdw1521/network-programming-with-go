package b_dial

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestDialContextCancelFanOut(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Second))

	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	defer func(listener net.Listener) {
		_ = listener.Close()
	}(listener)

	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	dial := func(ctx context.Context, address string, response chan int, id int, wg *sync.WaitGroup) {
		defer wg.Done()

		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", address)
		if err != nil {
			return
		}
		_ = c.Close()

		select {
		case <-ctx.Done():
		case response <- id:
		}
	}

	res := make(chan int)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go dial(ctx, listener.Addr().String(), res, i+1, &wg)
	}

	response := <-res
	cancel()
	wg.Wait()
	close(res)

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("expected canceled context; actual: %s", ctx.Err())
	}

	t.Logf("dialer %d retrieved the resource", response)
}
