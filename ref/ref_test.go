package ref

import (
	"bytes"
	"context"
	"errors"
	"reflect"
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

// ---------- 补充单元测试：指针/[]byte 参数、错误传播、方法过滤 ----------

type ptrService struct{}

func (s *ptrService) Echo(ctx context.Context, v *string) (string, error) {
	return *v, nil
}

func (s *ptrService) Pipe(ctx context.Context, v *[]byte) ([]byte, error) {
	return *v, nil
}

type flagService struct{}

func (s *flagService) Flip(ctx context.Context, b bool) (bool, error) {
	return !b, nil
}

type failService struct{}

func (s *failService) Fail(ctx context.Context) (string, error) {
	return "", errors.New("boom")
}

func (s *failService) OnlyErr(ctx context.Context) error {
	return nil
}

// filterService 包含各类不符合 RPC 约定的方法，用于验证 suitable_methods 过滤规则。
type filterService struct{}

func (s *filterService) Good(ctx context.Context) (string, error)          { return "ok", nil }
func (s *filterService) noCtx(v int) string                                { return "" }
func (s *filterService) noErr(ctx context.Context) string                  { return "" }
func (s *filterService) BadRet(ctx context.Context) (int, int, int, error) { return 0, 0, 0, nil }

func TestCallPointerStringArg(t *testing.T) {
	sf := Register(&ptrService{})
	rst, err := CallFuncWithContext(context.Background(), sf, "Echo", []byte("ptr"))
	if err != nil {
		t.Fatalf("call err: %v", err)
	}
	if string(rst) != "ptr" {
		t.Fatalf("unexpected result: %s", rst)
	}
}

func TestCallByteSliceArgs(t *testing.T) {
	sf := Register(&ptrService{})
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	rst, err := CallFuncWithContext(context.Background(), sf, "Pipe", payload)
	if err != nil {
		t.Fatalf("call err: %v", err)
	}
	if !bytes.Equal(rst, payload) {
		t.Fatalf("byte slice mismatch: %v", rst)
	}
}

func TestCallBoolArg(t *testing.T) {
	sf := Register(&flagService{})
	if _, err := CallFuncWithContext(context.Background(), sf, "Flip", []byte{1}); err != nil {
		t.Fatalf("call err: %v", err)
	}
}

func TestCallErrorPropagation(t *testing.T) {
	sf := Register(&failService{})
	if _, err := CallFuncWithContext(context.Background(), sf, "Fail"); err == nil || err.Error() != "boom" {
		t.Fatalf("expected 'boom' error, got %v", err)
	}
}

func TestCallOnlyErrorReturn(t *testing.T) {
	sf := Register(&failService{})
	if _, err := CallFuncWithContext(context.Background(), sf, "OnlyErr"); err == nil {
		t.Fatal("OnlyErr 只有一个返回值，当前协议要求 2 个返回值，应返回错误而非 panic")
	}
}

func TestSuitableMethodsFilter(t *testing.T) {
	sf := Register(&filterService{})
	if len(sf.M) != 1 {
		t.Fatalf("expected only Good registered, got %d methods", len(sf.M))
	}
	if _, ok := sf.M["Good"]; !ok {
		t.Fatal("Good should be registered")
	}
	for _, name := range []string{"noCtx", "noErr", "BadRet"} {
		if _, ok := sf.M[name]; ok {
			t.Fatalf("%s should be filtered out by suitable_methods", name)
		}
	}
	rst, err := CallFuncWithContext(context.Background(), sf, "Good")
	if err != nil || string(rst) != "ok" {
		t.Fatalf("Good call failed: rst=%s err=%v", rst, err)
	}
}

func TestInstanceParamsByteSlices(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	// 值 []byte
	v, err := instance_params(reflect.TypeOf([]byte{}), data)
	if err != nil {
		t.Fatalf("instance_params([]byte) err: %v", err)
	}
	if !bytes.Equal(v.Interface().([]byte), data) {
		t.Fatalf("[]byte mismatch: %v", v.Interface())
	}
	// 指针 *[]byte
	vp, err := instance_params(reflect.TypeOf(&[]byte{}), data)
	if err != nil {
		t.Fatalf("instance_params(*[]byte) err: %v", err)
	}
	if !bytes.Equal(*vp.Interface().(*[]byte), data) {
		t.Fatalf("*[]byte mismatch: %v", vp.Interface())
	}
}

func TestInstanceParamsUint8EmptyData(t *testing.T) {
	// 回归：空 data 的 uint8/int8 参数不得越界 panic
	var u8 uint8
	var i8 int8
	for _, typ := range []reflect.Type{
		reflect.TypeOf(uint8(0)),
		reflect.TypeOf(int8(0)),
		reflect.TypeOf(&u8),
		reflect.TypeOf(&i8),
	} {
		if _, err := instance_params(typ, nil); err != nil {
			t.Fatalf("instance_params(%v, nil) err: %v", typ, err)
		}
	}
	v, err := instance_params(reflect.TypeOf(uint8(0)), []byte{42})
	if err != nil {
		t.Fatalf("uint8 param err: %v", err)
	}
	if v.Uint() != 42 {
		t.Fatalf("uint8 should be 42, got %v", v.Uint())
	}
}

func TestBytesToUint64BigEndian(t *testing.T) {
	if v := bytes_to_uint64([]byte{0x01, 0x00}); v != 256 {
		t.Fatalf("bytes_to_uint64([]byte{1,0}) should be 256, got %d", v)
	}
	if v := bytes_to_uint64(nil); v != 0 {
		t.Fatalf("bytes_to_uint64(nil) should be 0, got %d", v)
	}
	if v := bytes_to_uint64([]byte{0xDE, 0xAD, 0xBE, 0xEF}); v != 0xDEADBEEF {
		t.Fatalf("uint32 value mismatch: %d", v)
	}
}

func TestBytesToFloatConversions(t *testing.T) {
	// 与 math.Float32bits 编码保持一致（大端）
	bits32 := []byte{0x3f, 0x80, 0x00, 0x00} // 1.0
	if v := bytes_to_float32(bits32); v != 1.0 {
		t.Fatalf("float32 should be 1.0, got %v", v)
	}
	if v := bytes_to_float32(nil); v != 0 {
		t.Fatalf("bytes_to_float32(nil) should be 0, got %v", v)
	}
	if v := bytes_to_float64(nil); v != 0 {
		t.Fatalf("bytes_to_float64(nil) should be 0, got %v", v)
	}
	if v := bytes_to_uintptr([]byte{0x01}); v != 1 {
		t.Fatalf("uintptr should be 1, got %v", v)
	}
}
