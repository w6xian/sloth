package frame

import (
	"encoding/json"
	"fmt"
)

// getSliceArray 将数据按指定大小分片(1024-65535)
func Split(n string, message []byte, sliceSize int, messageType byte) ([]*DataSlice, error) {
	// 这里可能有汉字
	// msg := []rune(string(message))
	msg := message
	totalSize := len(msg)
	sliceSize = max(sliceSize, 1024)
	sliceSize = min(sliceSize, 0xFFFF)
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
			P: byte(messageType),
			N: n,
			T: byte(totalSlice),
			I: byte(i),
			S: uint32(totalSize),
			D: msg[start:end],
		})
	}
	return slices, nil
}

func FromType(message []byte, messageType byte) (*DataSlice, error) {
	slices := &DataSlice{}
	switch messageType {
	case TextMessage:
		if err := json.Unmarshal(message, &slices); err != nil {
			return nil, err
		}
		return slices, nil

	case BinaryMessage:
		slices, err := Decode(message)
		if err != nil {
			return nil, err
		}
		return slices, nil
	}
	return nil, fmt.Errorf("invalid message type")

}
