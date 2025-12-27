package tlv

import (
	"sync"

	"github.com/w6xian/sloth/internal/lpool"
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
	pool       *lpool.BytePool
}

func NewOption(opts ...FrameOption) *Option {
	tlv_option_option.Do(func() {
		default_option = &Option{
			CheckCRC:   false,
			LengthSize: 1,
			MaxLength:  0x02,
			MinLength:  0x01,
			size:       make([]byte, 4),
			pool:       lpool.NewBytePool(100, 1024),
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
