package id

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/btcutil/base58"
	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
)

// snowflakeNodeMax snowflake 默认 NodeBits=10，节点号取值 [0,1023]。
const snowflakeNodeMax = 1023

var (
	// nodes 缓存 svr -> snowflake 节点。
	//
	// 原实现每次 NextId 都 snowflake.NewNode(svr)：
	//   1) 每次分配新 Node（GC 压力 + 锁初始化开销），位于每条 RPC 调用的热路径；
	//   2) 更严重的是新 Node 的 step 恒从 0 开始，同一毫秒内的多次调用会生成
	//      **完全相同的 ID**：callId 重复会让 WaitResult 把 A 的响应误判为 B 的响应
	//      （或把 B 的响应丢弃导致超时），是难以复现的串包 bug。
	// 复用节点后 step 在进程内单调推进，ID 全局唯一。
	nodesMu sync.RWMutex
	nodes   = make(map[int64]*snowflake.Node, 4)

	// fallbackSeq 仅在 snowflake 节点不可用（如 svr 越界）时兜底。
	fallbackSeq atomic.Uint64
)

type base58Encoder struct{}

func (enc base58Encoder) Encode(u uuid.UUID) string {
	return base58.Encode(u[:])
}

func (enc base58Encoder) Decode(s string) (uuid.UUID, error) {
	return uuid.FromBytes(base58.Decode(s))
}

func ShortStringID() string {
	enc := base58Encoder{}
	return enc.Encode(uuid.New())
}

func ShortID() string {
	return ShortStringID()
}

// node 返回 svr 对应的全局共享节点，不可用时返回 nil。
func node(svr int64) *snowflake.Node {
	nodesMu.RLock()
	n, ok := nodes[svr]
	nodesMu.RUnlock()
	if ok {
		return n
	}
	nodesMu.Lock()
	defer nodesMu.Unlock()
	if n, ok = nodes[svr]; ok {
		return n
	}
	n, err := snowflake.NewNode(svr)
	if err != nil {
		return nil
	}
	nodes[svr] = n
	return n
}

// NextId 生成一个全局唯一 ID。
//
// svr 取值必须在 [0,1023] 内，否则退化为"时间戳+进程内自增"的兜底 ID
// （保证进程内单调不重复，不会返回 0 造成调用方 ID 冲突）。
func NextId(svr int64) int64 {
	if svr >= 0 && svr <= snowflakeNodeMax {
		if n := node(svr); n != nil {
			if id := n.Generate(); id >= 0 {
				return id.Int64()
			}
		}
	}
	// 兜底：42 位毫秒时间戳 + 22 位进程内序号，进程内严格递增
	seq := fallbackSeq.Add(1) & 0x3FFFFF
	return int64((uint64(time.Now().UnixMilli()) << 22) | seq)
}
