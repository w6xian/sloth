// Package tools 是与 sloth 无关的杂项工具：cityhash。
//
// 主要入口：
//   - CityHash64 / CityHash32 / CityHash128：非加密哈希，快、不需要密钥，
//     只适合做分桶/校验（bucket 按 userId 分片用的就是 CityHash32），
//     **不要**用于安全场景
package tools
