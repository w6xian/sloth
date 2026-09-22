package utils

import (
	"fmt"
	"net/url"
	"strings"
)

func GetWsUrl(addr, path string) string {
	if strings.Contains(addr, "://") {
		u, err := url.Parse(addr)
		if err == nil {
			if u.Path == "" || u.Path == "/" {
				u.Path = path
			}
			return u.String()
		}
	}
	return fmt.Sprintf("ws://%s%s", addr, path)
}
