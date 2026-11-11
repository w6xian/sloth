package wire

// p2p 实例的线上协议（三端共用）。
//
// 信令面（ws，sloth）：peer 登记与牵线，走 RPC + 推送，报文是 JSON；
// 数据面（UDP，裸 socket）：纯文本协议，前缀区分消息类型，肉眼可读、
// tcpdump/wireshark 调试友好。演示用文本协议足够，生产打洞应换 KCP/QUIC。
//
// 打洞的核心前提：peer 用 **同一个 UDP socket** 既跟 coordinator 保持 REG
// 心跳、又对对方公网地址发 PUNCH 探针——cone 类 NAT 对同一 socket 的
// 出站映射是稳定的，对 coordinator 观测到的地址即是对端可直达的地址。
// Symmetric NAT 每个目的地址映射都不同，观测地址失效，打不通。

import (
	"fmt"
	"strconv"
	"strings"
)

// Proto 协议前缀（带版本号，留升级余地）。
const Proto = "P2P1"

const (
	PrefixReg   = Proto + " REG "   // peer → coordinator：登记/保活本端 UDP 地址
	PrefixAck   = Proto + " ACK "   // coordinator → peer：回观测到的公网地址
	PrefixPunch = Proto + " PUNCH " // 双方互发的打洞探针
	PrefixData  = Proto + " DATA "  // 打洞成功后的数据报文
)

// Intro coordinator 经 ws 信令推给双方的引见报文。
type Intro struct {
	Session  uint64 `json:"session"`   // 本次牵线的会话号
	PeerID   string `json:"peer_id"`   // 对端 ID
	PeerAddr string `json:"peer_addr"` // 对端公网 UDP 地址（coordinator 观测）
}

// Reg 构造 REG 报文。
func Reg(id string) []byte { return []byte(PrefixReg + id) }

// Ack 构造 ACK 报文（addr 为 coordinator 观测到的对端地址）。
func Ack(addr string) []byte { return []byte(PrefixAck + addr) }

// Punch 构造打洞探针。
func Punch(session, seq uint64) []byte {
	return []byte(fmt.Sprintf("%s%d %d", PrefixPunch, session, seq))
}

// Data 构造数据报文。
func Data(session uint64, text string) []byte {
	return []byte(fmt.Sprintf("%s%d %s", PrefixData, session, text))
}

// ParseReg 解析 REG 报文，返回 peer ID。
func ParseReg(s string) (string, bool) {
	rest, ok := trimPrefix(s, PrefixReg)
	return rest, ok && rest != ""
}

// ParseAck 解析 ACK 报文，返回观测到的地址。
func ParseAck(s string) (string, bool) {
	rest, ok := trimPrefix(s, PrefixAck)
	return rest, ok && rest != ""
}

// ParsePunch 解析探针，返回会话号。
func ParsePunch(s string) (uint64, bool) {
	rest, ok := trimPrefix(s, PrefixPunch)
	if !ok {
		return 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	session, err := strconv.ParseUint(fields[0], 10, 64)
	return session, err == nil
}

// ParseData 解析数据报文，返回会话号与文本。
func ParseData(s string) (uint64, string, bool) {
	rest, ok := trimPrefix(s, PrefixData)
	if !ok {
		return 0, "", false
	}
	fields := strings.SplitN(rest, " ", 2)
	if len(fields) == 0 {
		return 0, "", false
	}
	session, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	text := ""
	if len(fields) == 2 {
		text = fields[1]
	}
	return session, text, true
}

func trimPrefix(s, prefix string) (string, bool) {
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return strings.TrimSpace(s[len(prefix):]), true
}
