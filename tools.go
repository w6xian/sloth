package sloth

import (
	"fmt"
	"strings"
)

type RpcNode struct {
	Service string
	Method  string
}

// server.method 格式
func GetNode(svr string) (*RpcNode, error) {
	sm := strings.Split(svr, ".")
	if len(sm) != 2 {
		return nil, fmt.Errorf("service %s format error", svr)
	}
	return &RpcNode{
		Service: sm[0],
		Method:  sm[1],
	}, nil
}
