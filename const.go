package sloth

type Decoder func([]byte) ([]byte, error)
type Encoder func(any) ([]byte, error)
