package array

import "slices"

func InArray[T CommonType](e T, arr []T) bool {
	return slices.Contains(arr, e)
}
