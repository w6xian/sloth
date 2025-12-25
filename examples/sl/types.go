package main

import "fmt"

type StructName struct {
	Name string
}

func (s StructName) TlV() []byte {
	// T L V
	return append([]byte{0x3F, byte(len(s.Name))}, []byte(s.Name)...)
}

func get_tlv_struct_name(name string) []byte {
	fmt.Println(name)
	return append([]byte{0xFD, byte(len(name))}, []byte(name)...)
}
func get_tlv_struct_feild(name []byte, value []byte) []byte {
	t := []byte{0x3E, byte(len(name) + len(value))}
	t = append(t, name...)
	return append(t, value...)
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
