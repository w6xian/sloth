package wsocket

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"unicode/utf8"

	"github.com/w6xian/sloth/group"
	"github.com/w6xian/sloth/internal/tools"

	"github.com/gorilla/websocket"
)

const (
	TextMessage   = 0x1 // 文本数据消息
	BinaryMessage = 0x2 // 二进制数据消息
	CloseMessage  = 0x8 // 关闭控制消息
	PingMessage   = 0x9 // ping控制消息
	PongMessage   = 0xA // pong控制消息
)

func GetBucket(ctx context.Context, buckets []*group.Bucket, id int64) *group.Bucket {
	userIdStr := fmt.Sprintf("%d", id)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % uint32(len(buckets))
	return buckets[int64(idx)]
}

type DataSlice struct {
	// Name 分片名称（用于标识是哪个信息）
	N string `json:"n"`
	// Total 分片总数
	T int `json:"t"`
	// Index 当前分片索引
	I int `json:"i"`
	// Size 消息体总大小
	S int `json:"s"`
	// Data 分片数据
	D []byte `json:"d"`
}

var ids int32 = 0

func getSliceName() string {
	atomic.AddInt32(&ids, 1)
	if ids > 99 {
		atomic.StoreInt32(&ids, 0)
	}
	return fmt.Sprintf("%d", ids)
}

func (s *DataSlice) Bytes() []byte {
	return serialize(s)
}

func getSlice(message []byte) (DataSlice, error) {
	var slices DataSlice
	if err := json.Unmarshal(message, &slices); err != nil {
		return slices, err
	}
	return slices, nil
}

func getSliceArray(n string, message []byte, sliceSize int) ([]*DataSlice, error) {
	// 这里可能有汉字
	msg := []rune(string(message))
	totalSize := len(msg)
	totalSlice := totalSize / sliceSize
	if totalSize%sliceSize != 0 {
		totalSlice++
	}
	// 转换为字符串，判断真实长度

	slices := make([]*DataSlice, 0, totalSlice)
	for i := 0; i < totalSlice; i++ {
		start := i * sliceSize
		end := start + sliceSize
		end = min(end, totalSize)

		slices = append(slices, &DataSlice{
			N: n,
			T: totalSlice,
			I: i,
			S: totalSize,
			D: []byte(string(msg[start:end])),
		})
	}
	return slices, nil
}

// 分块发送数据
func slicesTextSend(n string, conn *websocket.Conn, data []byte, sliceSize int) error {
	// data 按大小分成多个块发送
	slices, err := getSliceArray(n, data, sliceSize)
	if err != nil {
		return err
	}
	// fmt.Println("slicesSend:", string(data))
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

func receiveMessage(conn *websocket.Conn, messageType int, message []byte) ([]byte, error) {
	sc, err := getSlice(message)
	if err != nil {
		return nil, err
	}
	id := sc.N
	dataSize := sc.S
	// 接收完整数据
	data := make([]byte, 0, dataSize)
	data = append(data, sc.D...)
	if dataSize == len(data) {
		return data, nil
	}

	for {
		msgType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				return nil, err
			}
		}
		if message == nil || msgType == -1 {
			return nil, fmt.Errorf("message is nil or msgType is -1")
		}
		slices, err := getSlice(message)
		if err != nil {
			return nil, err
		}

		if id != slices.N {
			return nil, fmt.Errorf("id not match")
		}
		data = append(data, slices.D...)
		realSize := utf8.RuneCountInString(string(data))
		if realSize == slices.S {
			return data, nil
		}
	}
}

func serialize(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}
