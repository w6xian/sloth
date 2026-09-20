package wsocket

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/decoder/fn"
	"github.com/w6xian/sloth/v4/decoder/frame"
	"github.com/w6xian/sloth/v4/nrpc"

	"github.com/gorilla/websocket"
	"github.com/w6xian/tlv"
)

// tlvMu 串行化对 tlv 库的调用。
//
// tlv 库本身不是并发安全的：newOption() 返回的是一个**全局共享**的 option 对象，
// 并且会往它的 encoder 字段里写（vendor/github.com/w6xian/tlv/option.go:32-36）。
// 多个 readPump goroutine 同时解析会数据竞争，还会互相污染编码状态。
// 依赖库改不了，只能在调用侧用一把锁把它串行起来。
var tlvMu sync.Mutex

// tlvValue 解出 TLV 帧的 Value 段。
//
// 两件事要兜住：
//  1. tlv 是外部库，Deserialize 对畸形输入会 panic（slice 越界）→ 转成 error；
//  2. tlv 库有共享状态，并发调用有数据竞争 → 走 tlvMu 串行化。
// 报文来自网络，解密/分片重组后仍可能是任意字节，失败一律按"不是 TLV 帧"
// 处理（原样透传），而不是把进程打挂。
func tlvValue(b []byte) (v []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = nil, fmt.Errorf("tlv decode panic: %v", r)
		}
	}()
	// fast-path：本框架的 FN 帧自带 magic，绝不可能是 TLV 帧。
	// 绝大多数入站消息都是 FN 帧，先判掉可以避开下面那把全局锁。
	if fn.HasMagic(b) {
		return nil, errors.New("not a tlv frame")
	}
	if len(b) < 2 {
		return nil, errors.New("too short for a tlv frame")
	}
	tlvMu.Lock()
	defer tlvMu.Unlock()
	f, e := tlv.Deserialize(b)
	if e != nil {
		return nil, e
	}
	return f.Value(), nil
}

// maxReassembleSize 分片重组后消息体的字节上限。
//
// dataSize 直接来自报文（frame 的 S 字段），若不设上限，一个声明几 GB 长度的
// 分片头会让下面的 make 一次性申请巨额内存（32 位平台上 uint32→int 还会溢出
// 成负数）。真实业务消息不会到这个量级，超限即判定为非法报文。
const maxReassembleSize = 64 << 20 // 64MB

const (
	TextMessage   = 0x1 // 文本数据消息
	BinaryMessage = 0x2 // 二进制数据消息
	CloseMessage  = 0x8 // 关闭控制消息
	PingMessage   = 0x9 // ping控制消息
	PongMessage   = 0xA // pong控制消息
)

// bucketKey 把 userId 格式化为分桶键。
//
// 热路径（每次 RPC 调用、每次广播都要算一次分桶）：原实现用 fmt.Sprintf("%d", id)
// + []byte(...) 转换，每次至少两次堆分配。int64 十进制最多 20 字符，
// 用栈上定长数组 + strconv.AppendInt 可做到零分配。
func bucketKey(id int64) []byte {
	var buf [20]byte
	return strconv.AppendInt(buf[:0], id, 10)
}

// GetBucket 保留原签名，分桶规则统一委托给 bucket.Pick（所有传输共用）。
func GetBucket(ctx context.Context, buckets []*bucket.Bucket, id int64) *bucket.Bucket {
	return bucket.Pick(buckets, id)
}

var ids int32 = 0

// sliceNames 预生成的 "00".."99"，避免每次发送都 fmt.Sprintf 分配字符串。
var sliceNames = func() (t [100]string) {
	for i := 0; i < 100; i++ {
		t[i] = string([]byte{byte('0' + i/10), byte('0' + i%10)})
	}
	return
}()

func getSliceName() string {
	n := atomic.AddInt32(&ids, 1)
	if n > 99 {
		atomic.StoreInt32(&ids, 0)
		n = 0
	}
	return sliceNames[n]
}

// 分块发送数据
func slicesTextSend(n string, conn *websocket.Conn, data []byte, sliceSize int) error {
	// 单分片快路径：绝大多数消息远小于分片上限，
	// 走 frame.Split 会额外分配 []*DataSlice 与 DataSlice 对象（每个分片一次堆分配），
	// 这里直接在栈上构造分片并编码，省掉这两处分配。
	if size := clampSliceSize(sliceSize); len(data) <= size {
		buf := frame.AppendSliceJSON(make([]byte, 0, 80+len(data)*4/3+8), frame.DataSlice{
			P: frame.TextMessage,
			N: n,
			T: 1,
			I: 0,
			S: uint32(len(data)),
			D: data,
		})
		w, err := conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return err
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
		return w.Close()
	}
	// data 按大小分成多个块发送
	slices, err := frame.Split(n, data, sliceSize, frame.TextMessage)
	if err != nil {
		return err
	}
	for _, slice := range slices {
		w, err := conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return err
		}
		w.Write(slice.Bytes())
		if err := w.Close(); err != nil {
			return err
		}
	}
	return nil
}

// clampSliceSize 与 frame.Split 内部的分片上限保持一致（[1024,65535]）。
func clampSliceSize(sliceSize int) int {
	if sliceSize < 1024 {
		return 1024
	}
	if sliceSize > 0xFFFF {
		return 0xFFFF
	}
	return sliceSize
}

func receiveMessage(conn nrpc.IReadConn, messageType byte, message []byte) ([]byte, error) {
	sc, err := frame.FromType(message, messageType)
	if err != nil {
		return nil, err
	}
	id := sc.N
	dataSize := sc.S
	// 长度声明来自报文：超限直接拒绝，避免按恶意 length 一次性申请巨额内存
	if dataSize > maxReassembleSize {
		return nil, fmt.Errorf("message size %d exceeds limit %d", dataSize, maxReassembleSize)
	}
	// 接收完整数据
	data := make([]byte, 0, dataSize)
	data = append(data, sc.D...)
	if int(dataSize) <= len(data) && sc.I == sc.T-1 {
		return data, nil
	}

	for {
		msgType, message, err := conn.ReadMessage()
		if err != nil {
			// 任何读错误（对端关闭、EOF、读超时、协议错误）都必须终止本次接收。
			// 原实现只对"非预期关闭"返回错误，其余错误会带着可能为 nil 的 message
			// 继续往下解析，既掩盖了真实的断连原因，也浪费一轮无效解析。
			return nil, err
		}
		if message == nil || msgType == CloseMessage || msgType == PingMessage || msgType == PongMessage {
			return nil, fmt.Errorf("message is nil or msgType is close or ping or pong message")
		}

		slices, err := frame.FromType(message, byte(msgType))
		if err != nil {
			return nil, err
		}

		if id != slices.N {
			return nil, fmt.Errorf("id not match")
		}
		data = append(data, slices.D...)
		// realSize := utf8.RuneCountInString(string(data))
		if int(dataSize) <= len(data) && slices.I == slices.T-1 {
			return data, nil
		}
	}
}
