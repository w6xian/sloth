package array

func Map[T any](arr []T, call func(v T) T) []T {
	var a []T
	for _, v := range arr {
		a = append(a, call(v))
	}
	return a

}
