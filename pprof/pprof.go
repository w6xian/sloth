package pprof

import (
	"context"
	"runtime"
	"sync"

	"github.com/w6xian/sloth/internal/utils"
)

type IPProf interface {
	PProf(ctx context.Context) (*BucketInfo, error)
}

// SystemInfo 系统信息
type PProfInfo struct {
	Version  string     `json:"version"`
	Buckets  int64      `json:"buckets"`
	Rooms    int64      `json:"rooms"`
	Connects int64      `json:"connects"`
	CpuNum   int64      `json:"cpu_num"`
	Mem      *Mem       `json:"mem"`
	Alloc    int64      `json:"alloc"`
	DiskSize int64      `json:"disk_size"`
	Position string     `json:"position"`
	_sync    sync.Mutex `json:"-"`
	_server  IPProf     `json:"-"`
}

type Mem struct {
	Alloc      int64 `json:"alloc"`
	TotalAlloc int64 `json:"total_alloc"`
	Sys        int64 `json:"sys"`
	HeapAlloc  int64 `json:"heap_alloc"`
	HeapSys    int64 `json:"heap_sys"`
	NumGC      int64 `json:"num_gc"`
}

type BucketInfo struct {
	Buckets  int64          `json:"buckets"`
	Rooms    map[int64]Room `json:"rooms"`
	Connects int64          `json:"connects"`
}

type Room struct {
	Id       int64 `json:"id"`
	Connects int64 `json:"connects"`
}

var pprofInfo *PProfInfo
var pprofOnce sync.Once

func New() *PProfInfo {
	pprofOnce.Do(func() {
		pprofInfo = &PProfInfo{
			_sync: sync.Mutex{},
		}
	})
	return pprofInfo
}

func (h *PProfInfo) Info(ctx context.Context) (*PProfInfo, error) {
	if h._server == nil {
		return h, nil
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	h.Mem = &Mem{
		Alloc:      int64(m.Alloc),
		TotalAlloc: int64(m.TotalAlloc),
		Sys:        int64(m.Sys),
		HeapAlloc:  int64(m.HeapAlloc),
		HeapSys:    int64(m.HeapSys),
		NumGC:      int64(m.NumGC),
	}
	return h, nil
}

func (h *PProfInfo) String() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	h.Mem = &Mem{
		Alloc:      int64(m.Alloc),
		TotalAlloc: int64(m.TotalAlloc),
		Sys:        int64(m.Sys),
		HeapAlloc:  int64(m.HeapAlloc),
		HeapSys:    int64(m.HeapSys),
		NumGC:      int64(m.NumGC),
	}
	return string(utils.Serialize(h))
}

func (h *PProfInfo) NewConeect() {
	h._sync.Lock()
	defer h._sync.Unlock()
	h.Connects++
}
func (h *PProfInfo) CloseConeect() {
	h._sync.Lock()
	defer h._sync.Unlock()
	h.Connects--
}

func (h *PProfInfo) UsePProf(server IPProf) {
	h._sync.Lock()
	defer h._sync.Unlock()
	h._server = server
}
