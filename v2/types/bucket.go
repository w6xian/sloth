package types

import "github.com/w6xian/sloth/v2/bucket"

type IBucket interface {
	Bucket(userId int64) *bucket.Bucket
}
