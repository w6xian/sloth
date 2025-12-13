package frame

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/w6xian/sloth/internal/utils"
)

const TextMessage byte = 0x01
const BinaryMessage byte = 0x02
const LongMessage byte = 0x80
const CRC byte = 0x40

type DataSlice struct {
	// P 消息类型 6位有效 高2位为扩展位 0x80 2字节长度 0x40 校验crc
	P byte `json:"p"`
	// Name 分片名称（用于标识是哪个信息）
	N string `json:"n"` // len 2
	// Total 分片总数
	T byte `json:"t"`
	// Index 当前分片索引
	I byte `json:"i"`
	// Size 消息体总大小
	S uint32 `json:"s"`
	// Data 分片数据
	D []byte `json:"d"`
}

func (s *DataSlice) Bytes() []byte {
	return serialize(s)
}

func (s *DataSlice) MuskCheck() byte {
	return s.P & 0x40
}

func (s *DataSlice) Encode() []byte {
	return Encode(s)
}

func get_header_size(lLen byte, checkCRC bool) byte {
	c := byte(0x02)
	if !checkCRC {
		c = 0
	}
	return lLen + 1 + 2 + 1 + 1 + c
}

func Encode(s *DataSlice, opts ...FrameOption) []byte {
	opt := newOption(opts...)
	// 1byte type
	// 2byte name
	// 1byte slices
	// 1byte index
	// 2/4byte size
	// 0/2byte crc
	// nbyte data
	tag := s.P & 0x3F
	checkCRC := s.P&CRC == CRC
	fmt.Println(tag, checkCRC)
	l := len(s.D)
	// 根据长度大小判断是否需要扩展tag
	if l <= 0xFFFF {
		opt.LengthSize = 2
	} else {
		tag |= LongMessage
		opt.LengthSize = 4
	}
	if opt.CheckCRC || checkCRC {
		checkCRC = true
		tag |= CRC
	}
	fmt.Println(tag, opt.LengthSize, checkCRC)
	// 根据长度大小判断是否需要扩展tag
	// 1+2+1+1+2/4+[2]+len(s.D)
	headerSize := get_header_size(opt.LengthSize, checkCRC)

	buf := make([]byte, int(headerSize)+len(s.D))
	buf[0] = tag
	name := []byte(s.N)[:2]
	buf[1] = name[0]
	buf[2] = name[1]
	buf[3] = byte(s.T)
	buf[4] = byte(s.I)
	lb := make([]byte, opt.LengthSize)
	if opt.LengthSize == 2 {
		binary.BigEndian.PutUint16(lb, uint16(len(s.D)))
	} else {
		binary.BigEndian.PutUint32(lb, uint32(len(s.D)))
	}
	if checkCRC {
		copy(buf[5:headerSize-2], lb)
		crc := utils.GetCrC(s.D)
		buf[headerSize-2] = crc[0]
		buf[headerSize-1] = crc[1]
	} else {
		copy(buf[5:headerSize], lb)
	}
	copy(buf[headerSize:], s.D)
	return buf
}

// DecodeSlice 从二进制数据中解码分片
func Decode(b []byte) (*DataSlice, error) {
	headerSize := get_header_size(2, false)
	if len(b) < int(headerSize) {
		return nil, fmt.Errorf("invalid slice data length")
	}
	tag := b[0]
	opt := newOption()
	if tag&LongMessage == 0 {
		opt.LengthSize = 2
	} else {
		opt.LengthSize = 4
	}
	if tag&CRC == CRC {
		opt.CheckCRC = true
	}

	l := uint32(binary.BigEndian.Uint16(b[5:7]))
	if opt.LengthSize == 4 {
		l = binary.BigEndian.Uint32(b[5:9])
	}
	headerSize = get_header_size(opt.LengthSize, opt.CheckCRC)
	if len(b) < int(headerSize)+int(l) {
		return nil, fmt.Errorf("invalid slice data length")
	}
	s := &DataSlice{
		P: tag,
		N: string(b[1:3]),
		T: b[3],
		I: b[4],
	}
	s.S = l

	data := b[headerSize : int(headerSize)+int(l)]
	// 校验crc
	if opt.CheckCRC {
		crc := b[headerSize-2 : headerSize]
		if !utils.CheckCRC(data, crc) {
			return nil, fmt.Errorf("invalid slice crc")
		}
	}
	s.D = data
	return s, nil
}

func serialize(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}
