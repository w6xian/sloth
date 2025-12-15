package sloth

import (
	"context"
	"sync"
)

// SystemInfo 系统信息
type PProfInfo struct {
	Version  string `json:"version"`
	Buckets  int64  `json:"buckets"`
	Rooms    int64  `json:"rooms"`
	Connects int64  `json:"connects"`
	CpuNum   int64  `json:"cpu_num"`
	MemSize  int64  `json:"mem_size"`
	DiskSize int64  `json:"disk_size"`
	Position string `json:"position"`
}

var pprofInfo *PProfInfo
var pprofOnce sync.Once

func NewPProfInfo() *PProfInfo {
	pprofOnce.Do(func() {
		pprofInfo = &PProfInfo{}
	})
	return pprofInfo
}

func (h *PProfInfo) Info(ctx context.Context) (any, error) {
	return h, nil
}
