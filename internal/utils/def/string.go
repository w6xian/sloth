package def

func GetString(i string, defualt string) string {
	if i == "" {
		return defualt
	}
	return i
}
