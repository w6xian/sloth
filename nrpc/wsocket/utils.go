package wsocket

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/decoder/frame"
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

func GetBucket(ctx context.Context, buckets []*bucket.Bucket, id int64) *bucket.Bucket {
	userIdStr := fmt.Sprintf("%d", id)
	idx := tools.CityHash32([]byte(userIdStr), uint32(len(userIdStr))) % uint32(len(buckets))
	return buckets[int64(idx)]
}

var ids int32 = 0

func getSliceName() string {
	atomic.AddInt32(&ids, 1)
	if ids > 99 {
		atomic.StoreInt32(&ids, 0)
	}
	return fmt.Sprintf("%d", ids)
}

// 分块发送数据
func slicesTextSend(n string, conn *websocket.Conn, data []byte, sliceSize int) error {
	// data 按大小分成多个块发送
	slices, err := frame.Split(n, data, sliceSize, frame.TextMessage)
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

// 分块发送数据
func slicesBinarySend(n string, conn *websocket.Conn, data []byte, sliceSize int) error {
	// data 按大小分成多个块发送
	slices, err := frame.Split(n, data, sliceSize, frame.BinaryMessage)
	if err != nil {
		return err
	}
	// fmt.Println("slicesSend:", string(data))
	for _, slice := range slices {
		w, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}
		w.Write(slice.Encode())
		if err := w.Close(); err != nil {
			return err
		}
	}
	return nil
}

func receiveMessage(conn *websocket.Conn, messageType byte, message []byte) ([]byte, error) {
	// fmt.Println("1receiveMessage:", messageType, string(message))
	sc, err := frame.FromType(message, messageType)
	if err != nil {
		return nil, err
	}
	// fmt.Println("2receiveMessage:", sc.N, sc.S, sc.I, sc.T, string(sc.D))
	id := sc.N
	dataSize := sc.S
	// 接收完整数据
	data := make([]byte, 0, dataSize)
	data = append(data, sc.D...)
	if int(dataSize) <= len(data) && sc.I == sc.T-1 {
		return data, nil
	}
	// fmt.Println("-----------")

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
