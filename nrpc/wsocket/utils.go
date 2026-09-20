package wsocket

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder/frame"
	"github.com/w6xian/sloth/v3/internal/tools"
	"github.com/w6xian/sloth/v3/nrpc"

	"github.com/gorilla/websocket"
)

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

func GetBucket(ctx context.Context, buckets []*bucket.Bucket, id int64) *bucket.Bucket {
	if len(buckets) == 0 {
		return nil
	}
	key := bucketKey(id)
	idx := tools.CityHash32(key, uint32(len(key))) % uint32(len(buckets))
	return buckets[idx]
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
