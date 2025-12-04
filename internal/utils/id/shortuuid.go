package id

import (
	"github.com/btcsuite/btcutil/base58"
	"github.com/bwmarrin/snowflake"
	"github.com/google/uuid"
	"github.com/lithammer/shortuuid/v4"
)

type base58Encoder struct{}

func (enc base58Encoder) Encode(u uuid.UUID) string {
	return base58.Encode(u[:])
}

func (enc base58Encoder) Decode(s string) (uuid.UUID, error) {
	return uuid.FromBytes(base58.Decode(s))
}
func ShortStringID() string {
	enc := base58Encoder{}
	return shortuuid.NewWithEncoder(enc)
}

func NextId(svr int64) int64 {
	node, err := snowflake.NewNode(svr)
	if err != nil {
		return 0
	}
	id := node.Generate()
	return id.Int64()
}
