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
		// 不超过4
		opt.MaxLength = min(4, maxSize)
		// 不小与1
		opt.MinLength = max(1, minSize)
	}
}

func MaxLength(maxSize byte) FrameOption {
	return func(opt *Option) {
		maxSize = min(4, maxSize)
		if opt.MinLength > maxSize {
			opt.MinLength, maxSize = maxSize, opt.MinLength
		}
		// 不超过4
		opt.MaxLength = min(4, maxSize)
	}
}

func MinLength(minSize byte) FrameOption {
	return func(opt *Option) {
		if opt.MinLength > minSize {
			opt.MinLength, minSize = minSize, opt.MinLength
		}
		minSize = min(4, minSize)
		// 不小与1
		opt.MinLength = max(1, minSize)
	}
}
