package ag

import (
	"encoding/json"
)

// jsonMarshalFallback Slice/Map/Struct 等复合类型用 encoding/json 转成字符串
// 作为 AG String 帧 payload；调用方再用 json.Unmarshal(Data(frame), &out) 还原。
func jsonMarshalFallback(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonUnmarshal(b []byte, out any) error {
	return json.Unmarshal(b, out)
}
