package tlv

type FrameOption func(opt *Option)

func CheckCRC() FrameOption {
	return func(opt *Option) {
		opt.CheckCRC = true
	}
}
