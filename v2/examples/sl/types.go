package main

type StructName struct {
	Name string
}

func (s StructName) TlV() []byte {
	// T L V
	return append([]byte{0x3F, byte(len(s.Name))}, []byte(s.Name)...)
}

type StructItem struct {
	Name string
	Type string
}

func (s StructItem) TlV() []byte {
	// T L V
	return []byte{0x01, 0x02, 0x03, 0x04}
}

// struct = []field
// 0xFF
// filed = name+value
// 0xFE 0x4F
// name(string)
// 0xFD
// value
// 0x10-0xE0
