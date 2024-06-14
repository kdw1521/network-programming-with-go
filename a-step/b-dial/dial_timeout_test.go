package b_dial

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

func DialTimeout(network, address string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{
		Control: func(_, addr string, _ syscall.RawConn) error {
			return &net.DNSError{
				Err:         "연결 타임 아웃!",
				Name:        addr,
				Server:      "127.0.0.1",
				IsTimeout:   true,
				IsTemporary: true,
			}
		},
		Timeout: timeout,
	}
	return d.Dial(network, address)
}

func TestDialTimeout(t *testing.T) {
	c, err := DialTimeout("tcp", "10.0.0.1:http", 5*time.Second)
	if err == nil {
		_ = c.Close()
		t.Fatal("연결이 정상적!")
	}

	var nErr net.Error
	ok := errors.As(err, &nErr)
	if !ok {
		t.Fatal(err)
	}
	if !nErr.Timeout() {
		t.Fatal("에러가 타임 아웃이 아님!")
	}
}
