// Package utils 是 sloth 对外开放的通用工具集：与传输层完全解耦，
// 不需要起连接、不依赖 sloth 任何类型，可单独 import 使用。
//
// 覆盖几类日常要写一遍的东西：
//   - 序列化：Serialize / Deserialize / JsonString / MapToStruct / IsJson
//   - 类型收敛：AnyToBytes / AnyToStr / MustAnyToBytes / MustAnyToStr
//     （把 any 统一成字节或字符串，省掉每个调用点各写一遍 type switch）
//   - 数值：Max / Min（泛型，覆盖所有整型与浮点）
//   - 校验与随机：GetCrC / CheckCRC / IsComplete、RandInt64
//   - ws 地址拼接：GetWsUrl
//
// 子包：
//   - utils/id：短 ID、随机串、雪花 ID
//   - utils/array：切片小工具（InArray / Map）
//
// 稳定性：本包属于 sloth 的公开 API，v4 内导出函数签名不变；
// 新增能力只加函数，不改既有签名。
//
// 为什么不放 internal/：这些函数业务代码天天要用，
// 放 internal 的话调用方（比如拿 utils.Serialize 组包）根本 import 不到。
package utils
