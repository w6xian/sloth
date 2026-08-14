//go:build ignore

package main

import (
	"encoding/hex"
	"fmt"
	"os"

	frame "github.com/w6xian/sloth/v3/decoder/frame"
)

type testCase struct {
	Name string
	S    *frame.DataSlice
	Opts []frame.FrameOption
}

func main() {
	cases := []testCase{
		{"short_noCRC", &frame.DataSlice{P: frame.TextMessage, N: "ab", T: 3, I: 1, S: 5, D: []byte("hello")}, nil},
		{"short_CRC_fromOpt", &frame.DataSlice{P: frame.BinaryMessage, N: "ab", T: 3, I: 1, S: 5, D: []byte("hello")}, []frame.FrameOption{frame.CheckCRC()}},
		{"short_CRC_fromP", &frame.DataSlice{P: frame.BinaryMessage | frame.CRC, N: "ab", T: 3, I: 1, S: 5, D: []byte("hello")}, nil},
		{"emptyName_noCRC", &frame.DataSlice{P: frame.TextMessage, N: "", T: 1, I: 0, S: 0, D: []byte{}}, nil},
		{"oneCharName_CRC", &frame.DataSlice{P: frame.TextMessage, N: "x", T: 1, I: 0, S: 4, D: []byte{0x11, 0x22, 0x33, 0x44}}, []frame.FrameOption{frame.CheckCRC()}},
		{"longNamePrefix_noCRC", &frame.DataSlice{P: frame.TextMessage, N: "xyz999", T: 10, I: 5, S: 2, D: []byte{0xDE, 0xAD}}, nil},
		{"shortBoundary_65535", &frame.DataSlice{P: frame.BinaryMessage, N: "AB", T: 1, I: 0, S: 0xFFFF, D: make([]byte, 0xFFFF)}, nil},
		{"longMessage_65536_noCRC", &frame.DataSlice{P: frame.BinaryMessage, N: "AB", T: 1, I: 0, S: 0x10000, D: make([]byte, 0x10000)}, nil},
	}
	fmt.Fprintln(os.Stderr, "===== Go Encode -> JSON cases =====")
	fmt.Print("[")
	first := true
	for _, c := range cases {
		if !first {
			fmt.Print(",")
		}
		first = false
		raw := frame.Encode(c.S, c.Opts...)
		dec, err := frame.Decode(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERR %s: %v\n", c.Name, err)
			os.Exit(1)
		}
		fmt.Printf("{\"name\":%q,\"p\":%d,\"n\":%q,\"t\":%d,\"i\":%d,\"s\":%d,\"dLen\":%d,\"dHead\":%q,\"hex\":%q,\"dHeadLast\":%q}",
			c.Name,
			dec.P, dec.N, dec.T, dec.I, dec.S, len(dec.D),
			hex.EncodeToString(dec.D[:min(len(dec.D), 8)]),
			hex.EncodeToString(raw),
			hex.EncodeToString(dec.D[max(len(dec.D)-8, 0):]),
		)
	}
	fmt.Println("]")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
