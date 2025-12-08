package utils

import "math/rand"

// RandInt64 随机整数
func RandInt64(min, max int64) int64 {
	if min >= max {
		return min
	}
	return min + rand.Int63n(max-min)
}
