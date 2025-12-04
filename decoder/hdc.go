package decoder

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
)

type HdCFrame []byte

// HdCFrameSize 固定头6字节 也是数小长度
const ID_SIZE = 8
const HdCFrameSize = 7 + ID_SIZE
const HDC_HEADER_SIZE = 7 + ID_SIZE
const POS_ID = 0
const POS_ADDRESS = 0 + ID_SIZE
const POS_FUNCTION_CODE = 1 + ID_SIZE
const POS_PICESE = 2 + ID_SIZE
const POS_LENGTH = 3 + ID_SIZE
const POS_CRC = 5 + ID_SIZE

// +------+-------+----+--------+--------+--------+
// | 地址  | 功能码 | 分片序号 | 数据长度 | CRC校验 |  数据内容|
// | 1字节 | 1字节  | 1字节    |  2字节   |   2字节 |   N字节 |
// +------+-------+---------+--------+--------+
type HdCHeader struct {
	Id           uint64 //Id(消息ID)
	Address      byte   //Address(地址)
	FunctionCode byte   //FunctionCode(功能码)
	Picese       byte   //Picese(分片序号)
	Length       uint16 //DataLength(数据长度)
	HdC          []byte //CRC校验
}

type HdC struct {
	h [HDC_HEADER_SIZE]byte
	d []byte
}

func (h *HdC) Id() uint64 {
	return binary.BigEndian.Uint64(h.h[POS_ID : POS_ID+ID_SIZE])
}

func (h *HdC) Address() byte {
	return h.h[POS_ADDRESS]
}
func (h *HdC) FunctionCode() byte {
	return h.h[POS_FUNCTION_CODE]
}
func (h *HdC) Length() uint16 {
	return binary.BigEndian.Uint16(h.h[POS_LENGTH : POS_LENGTH+2])
}
func (h *HdC) Crc() []byte {
	return h.h[POS_CRC:]
}
func (h *HdC) Header() []byte {
	return h.h[:HDC_HEADER_SIZE]
}
func (h *HdC) Picese() byte {
	return h.h[POS_PICESE]
}
func (h *HdC) Data() []byte {
	return h.d
}
func (h *HdC) Frame() []byte {
	return append(h.Header(), h.Data()...)
}

// GetHdCDataLength 获取HdC数据长度
func GetHdCDataLength(d []byte) *HdCHeader {
	if len(d) < HDC_HEADER_SIZE {
		return nil
	}
	return &HdCHeader{
		Id:           binary.BigEndian.Uint64(d[POS_ID : POS_ID+ID_SIZE]),
		Address:      d[POS_ADDRESS],
		FunctionCode: d[POS_FUNCTION_CODE],
		Picese:       d[POS_PICESE],
		Length:       binary.BigEndian.Uint16(d[POS_LENGTH : POS_LENGTH+2]),
		HdC:          d[POS_CRC : POS_CRC+2],
	}
}

func NewHdC(id uint64, address byte, functionCode byte, body []byte) *HdC {
	idBytes := make([]byte, ID_SIZE)
	binary.BigEndian.PutUint64(idBytes, id)
	a := &HdC{
		h: [HDC_HEADER_SIZE]byte{
			idBytes[0], idBytes[1], idBytes[2], idBytes[3], idBytes[4], idBytes[5], idBytes[6], idBytes[7],
			address,
			functionCode,
			0x01, //Picese(分片序号)
			1, 1,
		},
		d: body,
	}
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(len(a.d)))
	a.h[POS_LENGTH] = buf[0]
	a.h[POS_LENGTH+1] = buf[1]
	c := GetCrC(a.d)
	a.h[POS_CRC] = c[0]
	a.h[POS_CRC+1] = c[1]
	return a
}

func NewHdCReply(address byte, functionCode byte, body []byte) *HdC {
	return NewHdC(0, address, functionCode, body)
}

func IsHdCFrame(d []byte) bool {
	if len(d) < HDC_HEADER_SIZE {
		return false
	}
	Length := binary.BigEndian.Uint16(d[POS_LENGTH : POS_LENGTH+2])
	fmt.Println("IsHdCFrame: \n", Length, len(d) < HDC_HEADER_SIZE+int(Length))
	if len(d) < HDC_HEADER_SIZE+int(Length) {
		return false
	}
	// CRC
	if !CheckCRC(d[HDC_HEADER_SIZE:HDC_HEADER_SIZE+int(Length)], d[POS_CRC:POS_CRC+2]) {
		return false
	}
	return true
}

func DecodeHdC(d []byte) (*HdC, error) {
	Id := d[POS_ID : POS_ID+ID_SIZE]
	Address := d[POS_ADDRESS]
	FunctionCode := d[POS_FUNCTION_CODE]
	Length := binary.BigEndian.Uint16(d[POS_LENGTH : POS_LENGTH+2])
	Crc := d[POS_CRC : POS_CRC+2]
	data := d[HDC_HEADER_SIZE : HDC_HEADER_SIZE+int(Length)]
	// CRC
	if !CheckCRC(data, Crc) {
		log.Printf("crc check error %s %s\n", hex.EncodeToString(data), hex.EncodeToString(Crc))
		return nil, fmt.Errorf("crc check error")
	}
	return &HdC{
		h: [HDC_HEADER_SIZE]byte{Id[0], Id[1], Id[2], Id[3], Id[4], Id[5], Id[6], Id[7], Address, FunctionCode, d[POS_LENGTH], d[POS_LENGTH+1], Crc[0], Crc[1]},
		d: data,
	}, nil
}

func EncodeHdC(atlv *HdC) []byte {
	datasize := len(atlv.Data())
	buf := make([]byte, datasize+HDC_HEADER_SIZE)
	copy(buf[0:HDC_HEADER_SIZE], atlv.h[0:HDC_HEADER_SIZE])
	crc := GetCrC(atlv.Data())
	buf[POS_CRC] = crc[0]
	buf[POS_CRC+1] = crc[1]
	copy(buf[HDC_HEADER_SIZE:], atlv.Data())
	return buf
}
