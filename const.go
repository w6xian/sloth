package sloth

const (
	PROTOCOL_TLV = "TLV"
)

type Decoder func([]byte) ([]byte, error)
type Encoder func(any) ([]byte, error)
