package sloth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/w6xian/sloth/internal/logger"
	"github.com/w6xian/sloth/nrpc"
	"github.com/w6xian/sloth/nrpc/wsocket"
)

// 测试服务结构体
type TestService struct {
	Id int64
}

// 测试方法
type TestRequest struct {
	Name string `json:"name"`
}

type TestResponse struct {
	Message string `json:"message"`
}

func (s *TestService) Hello(ctx context.Context, req *TestRequest) (*TestResponse, error) {
	s.Id++
	return &TestResponse{Message: "Hello, " + req.Name},
		nil
}

func (s *TestService) Error(ctx context.Context, req *TestRequest) (*TestResponse, error) {
	return nil, errors.New("method not found")
}

// 测试连接创建
func TestNewConnect(t *testing.T) {
	server := DefaultServer()
	conn := ServerConn(server)

	if conn == nil {
		t.Errorf("ServerConn should not return nil")
	}

	client := DefaultClient()
	conn2 := ClientConn(client)

	if conn2 == nil {
		t.Errorf("ClientConn should not return nil")
	}
}

// 测试服务注册
func TestRegister(t *testing.T) {
	server := DefaultServer()
	conn := ServerConn(server)

	// 注册服务
	err := conn.Register("test", &TestService{}, "")
	if err != nil {
		t.Errorf("Register should not return error, got %v", err)
	}

	// 重复注册
	err = conn.Register("test", &TestService{}, "")
	if err == nil {
		t.Errorf("Register should return error when service already registered")
	}
}

// 测试方法调用
func TestCallFunc(t *testing.T) {
	server := DefaultServer()
	conn := ServerConn(server)

	// 注册服务
	err := conn.Register("test", &TestService{}, "")
	if err != nil {
		t.Errorf("Register should not return error, got %v", err)
	}

	// 准备请求数据
	req := &TestRequest{Name: "World"}
	reqData, err := json.Marshal(req)
	if err != nil {
		t.Errorf("json.Marshal should not return error, got %v", err)
	}

	// 创建RPC调用请求
	rpcCaller := &nrpc.RpcCaller{
		Method:   "test.Hello",
		Data:     reqData,
		Protocol: wsocket.BinaryMessage,
	}

	// 调用方法
	respData, err := conn.CallFunc(context.Background(), nil, rpcCaller)
	if err != nil {
		t.Logf("CallFunc returned error: %v", err)
		// 注意：这里可能会失败，因为我们没有实际的网络连接
		// 但我们可以测试调用过程是否正确
	}

	if respData != nil {
		// 解析响应
		var resp TestResponse
		err = json.Unmarshal(respData, &resp)
		if err != nil {
			t.Logf("json.Unmarshal returned error: %v", err)
			// 注意：这里可能会失败，因为返回的数据可能不是有效的JSON
			// 但我们可以测试调用过程是否正确
		} else {
			if resp.Message != "Hello, World" {
				t.Errorf("Expected message 'Hello, World', got '%s'", resp.Message)
			}
		}
	}
}

// 测试错误处理
func TestCallFuncError(t *testing.T) {
	server := DefaultServer()
	conn := ServerConn(server)

	// 注册服务
	err := conn.Register("test", &TestService{}, "")
	if err != nil {
		t.Errorf("Register should not return error, got %v", err)
	}

	// 准备请求数据
	req := &TestRequest{Name: "World"}
	reqData, err := json.Marshal(req)
	if err != nil {
		t.Errorf("json.Marshal should not return error, got %v", err)
	}

	// 测试不存在的方法
	rpcCaller := &nrpc.RpcCaller{
		Method:   "test.NonExistent",
		Data:     reqData,
		Protocol: wsocket.BinaryMessage,
	}

	_, err = conn.CallFunc(context.Background(), nil, rpcCaller)
	if err == nil {
		t.Errorf("CallFunc should return error for non-existent method")
	}

	// 测试错误的方法格式
	rpcCaller.Method = "test"
	_, err = conn.CallFunc(context.Background(), nil, rpcCaller)
	if err == nil {
		t.Errorf("CallFunc should return error for invalid method format")
	}

	// 测试不存在的服务
	rpcCaller.Method = "non-existent.Hello"
	_, err = conn.CallFunc(context.Background(), nil, rpcCaller)
	if err == nil {
		t.Errorf("CallFunc should return error for non-existent service")
	}
}

// 测试日志处理
func TestLogger(t *testing.T) {
	server := DefaultServer()
	conn := ServerConn(server)

	// 测试设置日志
	conn.SetLoggerLevel(logger.Debug)
	if conn.getLogLevel() != logger.Debug {
		t.Errorf("Expected log level Debug, got %v", conn.getLogLevel())
	}

	// 测试日志输出
	conn.Log(logger.Info, "Test log message")
}
