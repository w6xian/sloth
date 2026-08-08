package sloth

type RpcServer interface {
	Start(addr string) error
}

type IRpc interface {
	SetEncoder(encoder Encoder)
	SetDecoder(decoder Decoder)
}
