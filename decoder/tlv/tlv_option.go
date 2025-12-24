package tlv

type FrameOption func(opt *Option)

func CheckCRC() FrameOption {
	return func(opt *Option) {
		opt.CheckCRC = true
	}
}

func LengthSize(minSize, maxSize byte) FrameOption {
	return func(opt *Option) {
		if minSize > maxSize {
			minSize, maxSize = maxSize, minSize
		}
		opt.MaxLength = maxSize
		opt.MinLength = minSize
	}
}
