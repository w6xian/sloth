// Package tools 是与 sloth 无关的杂项工具：雪花 ID、随机 token、
// 会话 ID 拼接、SHA1、cityhash。
//
// 主要入口：
//   - GetSnowflakeId()：雪花 ID（注意：内部固定 node=1，多进程/多机部署
//     要自行改 node，否则 ID 有碰撞风险）
//   - GetRandomToken(n)：密码学随机的 token（crypto/rand）
//   - CityHash64 / CityHash32 / CityHash128：非加密哈希，快、不需要密钥，
//     只适合做分桶/校验，**不要**用于安全场景
//   - CreateSessionId / GetSessionIdByUserId / GetSessionName：会话 ID 拼装
package tools
