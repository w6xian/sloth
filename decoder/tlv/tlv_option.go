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
		opt.MaxLength = max(4, maxSize)
		opt.MinLength = min(4, minSize)
	}
}

func MaxLength(maxSize byte) FrameOption {
	return func(opt *Option) {
		maxSize = max(4, maxSize)
		if opt.MinLength > maxSize {
			opt.MinLength, maxSize = maxSize, opt.MinLength
		}
		opt.MaxLength = max(4, maxSize)
	}
}

func MinLength(minSize byte) FrameOption {
	return func(opt *Option) {
		if opt.MinLength > minSize {
			opt.MinLength, minSize = minSize, opt.MinLength
		}
		opt.MinLength = min(4, minSize)
	}
}
