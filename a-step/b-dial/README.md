# GOAL
```text
타임아웃

dial_test.go 의 클라이언트, 서버 연결에서는 문제가 있었다. 
각 연결 시도 시에 타임아웃을 운영체제의 타임아웃 시간에 의존해야했다.
예시로,
어떤 반응형 프로그램에 Dial을 호출했는데 연결이 되지않아 타임아웃이 되어야하는데,
운영체제가 타임아웃을 1시간 뒤에 시킨다면 UX가 아주 좋지않겠다.

이를 위해 코드상에서 타임아웃을 제어해본다.
```

---

## 코드 분석
### 1. DialTimeout 함수를 이용한 타임 아웃 (dial_timeout_test.go)
```go
/*
* net.DialTimeout 함수가 net.Dialer 인터페이스에 대한 제어권을 제공하지 않기 때문에 테스트코드에서는 불가능하므로 동일한 인터페이스를 갖는 별도의 구현체를 만든다.

net.Dialer 구조체를 초기화하고 
Control 필드는 함수로, 네트워크 연결을 제어하고 이 함수는 연결 타임아웃을 발생시키기 위해 net.DNSError를 반환한다.
Timeout 필드는 지정된 타임아웃 시간(timeout)을 설정한다.
*/
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

/*
이 테스트는 바로 끝난다.
위에서 DNSError 에서 바로 종료되도록 true 설정을 해두었기 때문에
*/
func TestDialTimeout(t *testing.T) {
    c, err := DialTimeout("tcp", "10.0.0.1:http", 5*time.Second)
    if err == nil {
        c.Close()
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
```

### 2. 데드라인 콘텍스트 로 타임아웃 (dial_context_test.go)
```go
func TestDialContext(t *testing.T) {
	dl := time.Now().Add(5 * time.Second) // 5초의 데드라인 설정
	ctx, cancel := context.WithDeadline(context.Background(), dl) // 데드라인을 갖는 ctx 생성
	defer cancel() // 함수 종료 시 cancel 호출해 ctx 해

	var d net.Dialer
	d.Control = func(_, _ string, _ syscall.RawConn) error {
		time.Sleep(5*time.Second + time.Millisecond) // 데드라인을 넘는 지연
		return nil
	}
	conn, err := d.DialContext(ctx, "tcp", "10.0.0.0:80")
	if err == nil {
		conn.Close()
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
```

### 3. 컨텐스트를 취소하여 연결 중단 (dial_cancel_test.go)
```go
func TestDialContextCancel(t *testing.T) {
    //  취소 가능(cancelable)한 컨텍스트 ctx와 이를 취소할 수 있는 함수 cancel을 생성.
    // context.Background()로 기본 컨텍스트를 제공.
	ctx, cancel := context.WithCancel(context.Background())

    // 고루틴이 완료되었음을 알리는 신호로 사용할 채널
	sync := make(chan struct{})

	go func() {
		defer func() { sync <- struct{}{} }()

		var d net.Dialer
		d.Control = func(_, _ string, _ syscall.RawConn) error {
			time.Sleep(time.Second) // 1초 지연
			return nil
		}

		conn, err := d.DialContext(ctx, "tcp", "10.0.0.1:80")
		if err != nil {
			t.Log(err)
			return
		}

		conn.Close()
		t.Error("타임 아웃 안났다!")
	}()

	cancel() // 메인 고루틴에서 바로 종료
	<-sync

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("expected canceled context; actual: %q", ctx.Err())
	}
}
/*
코드 흐름
1. 취소 가능 컨텍스트를 생성하고, 동기화 채널 sync를 생성.
2. 고루틴을 시작하여 1초 동안 슬립한 후 연결을 시도.
3. 연결 시도가 이루어지기 전에 컨텍스트를 취소.
4. 고루틴이 종료될 때까지 대기.
5. 컨텍스트의 에러가 context.Canceled인지 확인.
*/
```

### 4. 다중 다이얼러 취소
```go
func TestDialContextCancelFanOut(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Second))

	listener, err := net.Listen("tcp", "127.0.0.1:")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() { // 새로운 고루틴을 시작하여 클라이언트 연결을 수락.
		conn, err := listener.Accept()
		if err == nil { // 연결 수락시 바로 닫아버림
			conn.Close()
		}
	}()

	// 네트워크 연결 시도 함수
	dial := func(ctx context.Context, address string, response chan int, id int, wg *sync.WaitGroup) {
		defer wg.Done() // 함수 종료 시 WaitGroup의 작업을 종료 시킴

		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", address)
		if err != nil {
			return
		}
		c.Close()

		// 연결이 성공하면 연결을 닫고, 컨텍스트가 완료되었는지 확인한 후, 완료되지 않았다면 응답 채널 response에 ID를 전송.
		select {
		case <-ctx.Done():
		case response <- id:
		}
	}

	res := make(chan int) // 결과를 받을 채널
	var wg sync.WaitGroup // 여러 고루틴의 작업이 완료될 때까지 기다리는 데 사용

	for i := 0; i < 10; i++ {
		wg.Add(1) // 각 고루틴이 시작될 때 WaitGroup의 카운터를 증가
		go dial(ctx, listener.Addr().String(), res, i+1, &wg) // dial 실행
	}

    // res 채널에서 첫 번째 응답을 받은 후, cancel 함수를 호출하여 컨텍스트를 취소.
	response := <-res
	cancel()

	wg.Wait() // 모든 고루틴의 작업이 완료될 때까지 대기
	close(res) // res 채널 닫음

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("expected canceled context; actual: %s", ctx.Err())
	}

	t.Logf("dialer %d retrieved the resource", response)
}
```

---

## 개인적 고찰 (왜? WHY?)
