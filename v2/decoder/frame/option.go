package frame

type FrameOption func(opt *Option)

func CheckCRC() FrameOption {
	return func(opt *Option) {
		opt.CheckCRC = true
	}
}

type Option struct {
	CheckCRC   bool
	LengthSize byte
}

func newOption(opts ...FrameOption) Option {
	opt := Option{
		CheckCRC:   false,
		LengthSize: 2,
	}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}
