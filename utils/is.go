package utils

func IsJson(data []byte) bool {
	if len(data) < 2 {
		return false
	}
	f := data[0]
	return (f == '{' || f == '[')
}
