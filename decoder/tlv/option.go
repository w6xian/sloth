package tlv

import (
	"sync"
)

type Bin []byte
type TLVFrame []byte

var tlv_option_option sync.Once
var default_option *Option

type Option struct {
	CheckCRC   bool
	LengthSize byte
	MaxLength  byte
	MinLength  byte
	EmptyFrame []byte
	size       []byte
	// pool        *lpool.BytePool
	encoder *encodeState
}

func NewOption(opts ...FrameOption) *Option {
	tlv_option_option.Do(func() {
		default_option = &Option{
			CheckCRC:   false,
			LengthSize: 1,
			MaxLength:  0x02,
			MinLength:  0x01,
			size:       make([]byte, 4),
		}
		default_option.EmptyFrame = make([]byte, default_option.MinLength+1)
		default_option.encoder = newEncodeState()
	})
	opt := default_option
	for _, o := range opts {
		o(opt)
	}
	if opt.LengthSize < opt.MinLength {
		opt.LengthSize = opt.MinLength
	}
	if opt.LengthSize > opt.MaxLength {
		opt.LengthSize = opt.MaxLength
	}
	return opt
}

func newOption(opts ...FrameOption) *Option {
	tlv_option_option.Do(func() {
		default_option = &Option{
			CheckCRC:   false,
			LengthSize: 1,
			MaxLength:  0x02,
			MinLength:  0x01,
			size:       make([]byte, 4),
		}
		default_option.EmptyFrame = make([]byte, default_option.MinLength+1)
	})
	opt := default_option
	for _, o := range opts {
		o(opt)
	}
	if opt.LengthSize < opt.MinLength {
		opt.LengthSize = opt.MinLength
	}
	if opt.LengthSize > opt.MaxLength {
		opt.LengthSize = opt.MaxLength
	}
	opt.encoder = newEncodeState()
	return opt
}

func (opt *Option) CheckCRCOption() FrameOption {
	return func(o *Option) {
		o.CheckCRC = opt.CheckCRC
	}
}

func (opt *Option) MaxLengthOption() FrameOption {
	return func(o *Option) {
		o.MaxLength = opt.MaxLength
	}
}

func (opt *Option) MinLengthOption() FrameOption {
	return func(o *Option) {
		o.MinLength = opt.MinLength
	}
}

func (opt *Option) GetEncoder() *encodeState {
	return newEncodeState()
}
func (opt *Option) PutEncoder(es *encodeState) {
	encodeStatePool.Put(es)
}

func (opt *Option) WriteByte(tag byte) error {
	opt.encoder.LengthSize++
	return opt.encoder.WriteByte(tag)
}
func (opt *Option) Write(data []byte) (int, error) {
	opt.encoder.LengthSize += len(data)
	return opt.encoder.Write(data)
}

func (opt *Option) Bytes() []byte {
	return opt.encoder.Bytes()
}

func (opt *Option) Encoder() *encodeState {
	return opt.encoder
}
