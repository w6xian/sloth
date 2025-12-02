package def

func GetNumber[I int | int64 | uint | uint64](i I, defualt I) I {
	if i == 0 {
		return defualt
	}
	return i
}
