package ref

import (
	"context"
	"testing"
)

type echoService struct{}

func (s *echoService) Hello(ctx context.Context, name string) (string, error) {
	return "hello:" + name, nil
}

func (s *echoService) Add(ctx context.Context, a int, b int) (int, error) {
	return a + b, nil
}

func TestRegisterAndCall(t *testing.T) {
	sf := Register(&echoService{})
	if len(sf.M) == 0 {
		t.Fatal("no methods registered")
	}
	rst, err := CallFuncWithContext(context.Background(), sf, "Hello", []byte("world"))
	if err != nil {
		t.Fatalf("call err: %v", err)
	}
	if string(rst) != "hello:world" {
		t.Fatalf("unexpected result: %s", rst)
	}
}

func TestCallMultiIntArgs(t *testing.T) {
	sf := Register(&echoService{})
	rst, err := CallFuncWithContext(context.Background(), sf, "Add", []byte{3}, []byte{4})
	if err != nil {
		t.Fatalf("call err: %v", err)
	}
	if string(rst) != "7" {
		t.Fatalf("unexpected result: %s", rst)
	}
}

func TestCallMethodNotFound(t *testing.T) {
	sf := Register(&echoService{})
	if _, err := CallFuncWithContext(context.Background(), sf, "NotExist"); err == nil {
		t.Fatal("expected method not found error")
	}
}

func TestCallTooManyArgs(t *testing.T) {
	sf := Register(&echoService{})
	if _, err := CallFuncWithContext(context.Background(), sf, "Hello", []byte("a"), []byte("b")); err == nil {
		t.Fatal("expected too many arguments error")
	}
}

func TestBytesToBoolEmpty(t *testing.T) {
	if bytes_to_bool(nil) != false {
		t.Fatal("bytes_to_bool(nil) should be false")
	}
	if !bytes_to_bool([]byte{1}) {
		t.Fatal("bytes_to_bool([]byte{1}) should be true")
	}
	if bytes_to_bool([]byte{0}) {
		t.Fatal("bytes_to_bool([]byte{0}) should be false")
	}
}

func TestBytesToIntBoundary(t *testing.T) {
	if v := bytes_to_int([]byte{0, 0, 0}); v != 0 {
		t.Fatalf("bytes_to_int(3 bytes) should be 0, got %d", v)
	}
	if v := bytes_to_int64([]byte{1, 0}); v != 256 {
		t.Fatalf("bytes_to_int64([]byte{1,0}) should be 256, got %d", v)
	}
	if v := bytes_to_int64([]byte{0, 1}); v != 1 {
		t.Fatalf("bytes_to_int64([]byte{0,1}) should be 1, got %d", v)
	}
	if bytes_to_bool([]byte{}) != false {
		t.Fatal("bytes_to_bool(empty) should be false")
	}
}
