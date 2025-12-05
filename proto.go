package sloth

import (
	"encoding/base64"
	"fmt"
)

func DecodeString(frame []byte) string {
	decoded, err := base64.StdEncoding.DecodeString(string(frame))
	if err != nil {
		fmt.Println("Error decoding:", err)
		return ""
	}
	return string(decoded)
}
