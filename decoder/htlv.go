// HTLV+CRC, H header code, T function code, L data length, V data content
//+------+-------+-------+--------+--------+
//| H    | T     | L     | V      | CRC    |
//| 1Byte| 1Byte | 1Byte | NBytes | 2Bytes |
//+------+-------+-------+--------+--------+

// HeaderCode FunctionCode DataLength Body                         CRC
// A2         10           0E         0102030405060708091011121314 050B
//
//
// Explanation:
// 1. The data length len is 14 (0E), where len only refers to the length of the Body.
//
//
// lengthFieldOffset = 2 (the index of len is 2, starting from 0) The offset of the length field
// lengthFieldLength = 1 (len is 1 byte) The length of the length field in bytes
// lengthAdjustment = 2 (len only represents the length of the Body, the program will only read len bytes and end, but there are still 2 bytes of CRC to read, so it's 2)
// initialBytesToStrip = 0 (this 0 represents the complete protocol content. If you don't want A2, then it's 1) The number of bytes to strip from the decoding frame for the first time
// maxFrameLength = 255 + 4 (starting code, function code, CRC) (len is 1 byte, so the maximum length is the maximum value of an unsigned byte plus 4 bytes)

// [简体中文]
//
// HTLV+CRC，H头码，T功能码，L数据长度，V数据内容
//+------+-------+---------+--------+--------+
//| 头码  | 功能码 | 数据长度 | 数据内容 | CRC校验 |
//| 1字节 | 1字节  | 1字节   | N字节   |  2字节  |
//+------+-------+---------+--------+--------+

//头码   功能码 数据长度      Body                         CRC
//A2      10     0E        0102030405060708091011121314 050B
//
//
//   说明：
//   1.数据长度len是14(0E),这里的len仅仅指Body长度;
//
//
//   lengthFieldOffset   = 2   (len的索引下标是2，下标从0开始) 长度字段的偏差
//   lengthFieldLength   = 1   (len是1个byte) 长度字段占的字节数
//   lengthAdjustment    = 2   (len只表示Body长度，程序只会读取len个字节就结束，但是CRC还有2byte没读呢，所以为2)
//   initialBytesToStrip = 0   (这个0表示完整的协议内容，如果不想要A2，那么这里就是1) 从解码帧中第一次去除的字节数
//   maxFrameLength      = 255 + 4(起始码、功能码、CRC) (len是1个byte，所以最大长度是无符号1个byte的最大值)

package decoder

import (
	"encoding/binary"
	"encoding/hex"
	"log"

	"github.com/w6xian/sloth/internal/utils"
)

const HEADER_SIZE = 6

// modbus RTU
// +------+-------+---------+--------+--------+
// | 地址  | 功能码 | 数据长度 | 数据内容 | CRC校验 |
// | 1字节 | 1字节  | 2字节   | N字节    |  2字节  |
// +------+-------+---------+--------+--------+
type ATLVCrcDecoder struct {
	Address      byte   //Address(地址)
	FunctionCode byte   //FunctionCode(功能码)
	Length       uint16 //DataLength(数据长度)
	Crc          []byte //CRC校验
	Body         []byte //BodyData(数据内容)
}

func NewATLVCrcDecoder(address byte, functionCode byte, body []byte) *ATLVCrcDecoder {
	a := &ATLVCrcDecoder{
		Address:      address,
		FunctionCode: functionCode,
		Length:       uint16(len(body) + 2),
		Body:         body,
		Crc:          []byte{},
	}
	a.Crc = utils.GetCrC(a.Body)
	return a
}

func DecodeATLVCrc(data []byte) *ATLVCrcDecoder {
	datasize := len(data)
	htlvData := ATLVCrcDecoder{}
	// Parse the header
	htlvData.Address = data[0]
	htlvData.FunctionCode = data[1]
	htlvData.Length = binary.BigEndian.Uint16(data[2:4])
	htlvData.Body = data[4 : datasize-2]
	htlvData.Crc = data[datasize-2 : datasize]
	// CRC
	if !utils.CheckCRC(htlvData.Body, htlvData.Crc) {
		log.Printf("crc check error %s %s\n", hex.EncodeToString(data), hex.EncodeToString(htlvData.Crc))
		return nil
	}
	return &htlvData
}
func EncodeATLVCrc(atlv *ATLVCrcDecoder) []byte {
	datasize := len(atlv.Body)
	buf := make([]byte, datasize+HEADER_SIZE)
	buf[0] = atlv.Address
	buf[1] = atlv.FunctionCode
	binary.BigEndian.PutUint16(buf[2:4], uint16(datasize)+2)
	copy(buf[4:datasize+4], atlv.Body)
	crc := utils.GetCrC(atlv.Body)
	copy(buf[datasize+4:], crc)
	return buf
}
