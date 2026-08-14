/**
 * ============================================================
 * SockRpcV3 —— 浏览器端 V3 WebSocket RPC 客户端
 * ============================================================
 *
 * 【设计目标】
 *   1. 严格对齐 Go 服务端 nrpc/wsocket/ws_server.go#L327 读泵的协议解析顺序：
 *        ReadMessage → receiveMessage(分片聚合)
 *                   → tlv.Deserialize(可选项，若为 TLV 帧则剥离外层取 Value)
 *                   → fn.Action(m) 校验 FN 帧
 *                   → s.HandleFn(按 Action 分发: CALL / REPLY_SUCCESS / REPLY_ERROR / BROADCAST)
 *
 *   2. 严格对齐 Go 客户端 examples/ws/node/main.go#L76 的调用链：
 *        client.Call(ctx, "v1.Sign", []byte("sign"))
 *        → slots.Client 包装层: 对每个 arg 执行 ag.Encode(arg)
 *        → WsChannelClient.Call(header, "v1.Sign", ...[][]byte)
 *            ├─ msg = JsonCallObject{ Header, Method, Args:[][]byte{agEncArg...}, Data, Error }
 *            ├─ utils.Serialize(msg)                (json.Marshal → UTF-8 bytes)
 *            ├─ fn.Encode(ACTION_CALL, callId, jsonBytes)  (FN 帧 @F+Action+ID+Len+Data)
 *            └─ slicesTextSend(name, conn, fnBytes, sliceSize=512) (TextMessage 模式分片)
 *
 * 【四层协议栈 (请求方向：浏览器 → 服务器)】
 *   ① AG 参数帧：每个入参独立编码为 MAGIC":p"+Type(1B)+Len(2B BE)+Value
 *        → 对应文件：ag.js (已与 Go decoder/ag 双编解码全 PASS)
 *   ② JsonCallObject：Header / Method / Args[][]byte / Data[]byte / Error
 *        → 对应文件：message/message.go#L7-L13  (结构体定义)
 *        → JSON 序列化约定：Go 的 json.Marshal 对 []byte / [][]byte 会自动编码为 Base64 字符串，
 *          浏览器端必须保持一致（手动把 Uint8Array 先转 Base64 再放入 JSON 对象）。
 *   ③ FN 传输帧：MAGIC"@F"(2B) + Action(1B) + ID(uint64 BE, 8B) + Length(uint32 BE, 4B) + Data(N B)
 *        → HeaderSize = 15 字节
 *        → 对应文件：decoder/fn/fn.go + 本目录 fn.js （刚修复浏览器兼容）
 *   ④ DataSlice 分片 (TextMessage 模式)：
 *        JSON 对象 { P: 1, N: "00", T: totalSlices, I: curIdx, S: totalBytes, D: base64(sliceBytes) }
 *        → Go 通过 json.Unmarshal(message, *DataSlice) 解析：D 字段 []byte 自动 Base64 解码
 *        → Go 切片默认 512 字节 (utils.go slicesTextSend)；片名 %02d 递增，过 99 归零 (utils.go getSliceName)
 *        → 对应文件：decoder/frame/utils.go Split / FromType + 本目录 slice.js
 *
 * 【反向协议栈 (服务器 → 浏览器)】
 *   分片聚合(N 相同) → 完整 bytes → 可选 TLV 剥离 → fn.Decode → Action 分发：
 *     · ACTION_REPLY_SUCCESS(0x02)：ID 匹配 Call 的 pending 回调，Data 为 AG 帧 → 自动 AG.Decode 还原 JS 值
 *     · ACTION_REPLY_ERROR (0x03)：ID 匹配 pending 的 errCb，Data = UTF-8 错误描述
 *     · ACTION_CALL        (0x01)：服务器反向调用客户端 Bind 的本地服务
 *                                  Body = JsonCallObject JSON → Args[i] Base64→Uint8Array→AG.Decode→JS 值
 *     · ACTION_BROADCAST  (0xFF)：无 ID，Data (自动 AG.Decode) 广播给所有 OnMessage 监听器
 *
 * 【浏览器环境依赖】
 *   需要在 HTML 中按以下顺序加载前置文件：
 *     <script src="tools.js"></script>     (提供 zeroExtendN / getCRC / writeUint64BE 等工具)
 *     <script src="slice.js"></script>     (DataSlice 二进制帧 Encode/Decode，用于 BinaryMessage 模式)
 *     <script src="ag.js"></script>        (AG 参数帧 Encode/Decode → window.AG)
 *     <script src="fn.js"></script>        (FN 传输帧 Encode/Decode → window.Fn)
 *     <script src="sock_rpc_v3.js"></script> (本文件 → window.SockRpcV3 / 全局 ACTION_* 常量)
 *   注：即使加载顺序错乱，fn.js 内部已兜底 writeUint64BE/readUint64BE 实现，不会崩。
 *
 * ============================================================
 */

/* ============================================================
 *  Action 常量（对齐 actions/actions.go L3-L12，保持与 V2 完全同名）
 * ============================================================ */
const ACTION_CALL          = 0x01; // 调用（请求+响应配对）
const ACTION_REPLY_SUCCESS = 0x02; // 响应：成功
const ACTION_REPLY_ERROR   = 0x03; // 响应：错误
const ACTION_INVALID       = 0x00; // 非法操作 (占位)
const ACTION_BROADCAST     = 0xFF; // 广播 (无 ID)

/* ============================================================
 *  DataSlice 模式常量 (对齐 nrpc/wsocket/utils.go L16-L22)
 * ============================================================ */
const _SLICE_TEXT   = 0x01; // 同 Go websocket.TextMessage
const _SLICE_BINARY = 0x02; // 同 Go websocket.BinaryMessage
const _DEFAULT_SLICE_SIZE = 512; // 与 V2 sock_rpc_v2.js + Go ws_client 保持一致

/* ============================================================
 *  TLV CRC 帧（最外层包装层，对齐 vendor/github.com/w6xian/tlv 包）
 *
 *  【帧结构】：
 *     [0] tag 字节
 *        bit 8 (0x80)  = 1  表示 Length 字段用 2 字节，否则 1 字节
 *        bit 7 (0x40)  = 1  表示包含 CRC（2 字节，Value 之前）；否则无 CRC
 *        bit 6~1        实际类型 tag（0x01~0x40），我们 FN 数据统一用 0x12=TLV_TYPE_BYTE
 *     [1:1+lenN] length  = BE uint{lenN}  (lenN = 1 或 2)
 *     [1+lenN : 1+lenN+2] CRC  （仅 bit7=1 时，2 字节 [lo,hi]，CRC 仅覆盖 Value）
 *     [1+lenN+2*crc : end] Value = FN 帧（或其他 payload）
 *
 *  【为什么必须有这层？】（参考 ws_server.go L359-L363）
 *    服务端先执行 `tlvFrame, err := tlv.Deserialize(m)` → err==nil 则 m = tlvFrame.Value()，
 *    再 `fn.Action(m)`。我们 FN 帧的首字节恰好是 0x40（字符 @），它会被 tlv 当成：
 *      tag=0x40 → bit7=0x40=1 (CheckCRC=1)，bit8=0 (Length=1B)
 *    → 服务端尝试按「1B Length + 2B CRC + Value」解析，得到的 CRC 与实际计算不符
 *    → `invalid crc`，服务端直接丢弃该帧，不回包 → 浏览器端 10s 超时。
 *    因此浏览器必须在 FN 帧外面主动用【合法的 TLV】 包一层，tag 要避开 0x40，
 *    并且如果开启 CRC 就必须真的计算正确 CRC（我们统一开 CRC）。
 * ============================================================ */
// TLV 类型常量（vendor tlv/tlv.go L27-L77）
const _TLV_TYPE_INT    = 0x01;
const _TLV_TYPE_UINT8  = 0x07;
const _TLV_TYPE_BYTE   = 0x12;
const _TLV_TYPE_STRING = 0x13;
const _TLV_TYPE_JSON   = 0x14;
// TLV 错误常量（解包时用）
const _TLV_ERR_INVALID_LEN = 'tlv: invalid length or frame too short';
const _TLV_ERR_INVALID_CRC = 'tlv: invalid crc';
const _TLV_HEADER_MIN_SIZE = 2;

/**
 * 计算 TLV CRC（与 Go vendor tlv.GetCrC 完全一致：CRC16-CCITT-FALSE 查表变体，输出 [lo,hi]）
 * 优先复用 tools.js 已经声明好的全局 getCRC/crc16_h/crc16_l；缺失则用内联查表。
 * @param {Uint8Array} data
 * @returns {Uint8Array} [lo, hi] 2 字节
 */
function _tlv_calc_crc(data) {
    if (typeof getCRC === 'function' && typeof crc16_h !== 'undefined' && typeof crc16_l !== 'undefined') {
        const r = getCRC(data);
        return (r && r.length === 2) ? r : _tlv_calc_crc_fallback(data);
    }
    return _tlv_calc_crc_fallback(data);
}
function _tlv_calc_crc_fallback(data) {
    // 内联与 tools.js / Go crc.go 同款表（crc16_h / crc16_l 仅取 256 项即可，用 IIFE 初始化一次）
    if (typeof _tlv_crc_tab_h === 'undefined') {
        _tlv_crc_init_tab();
    }
    let hi = 0x00ff;
    let low = 0x00ff;
    for (let i = 0; i < data.length; i++) {
        const pos = (low ^ (data[i] & 0xff)) & 0x00ff;
        low = (hi ^ _tlv_crc_tab_h[pos]) & 0xff;
        hi  = _tlv_crc_tab_l[pos] & 0xff;
    }
    const d_crc = (((hi & 0xff) << 8) | (low & 0xff)) & 0xffff;
    const out = new Uint8Array(2);
    out[0] = (d_crc & 0xff);
    out[1] = ((d_crc >> 8) & 0xff);
    return out;
}
var _tlv_crc_tab_h, _tlv_crc_tab_l;
function _tlv_crc_init_tab() {
    _tlv_crc_tab_h = new Uint8Array([
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,
        0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,
        0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41,0x00,0xc1,0x81,0x40,0x01,0xc0,0x80,0x41
    ]);
    _tlv_crc_tab_l = new Uint8Array([
        0x00,0xc0,0xc1,0x01,0xc3,0x03,0x02,0xc2,0xc6,0x06,0x07,0xc7,0x05,0xc5,0xc4,0x04,
        0xcc,0x0c,0x0d,0xcd,0x0f,0xcf,0xce,0x0e,0x0a,0xca,0xcb,0x0b,0xc9,0x09,0x08,0xc8,
        0xd8,0x18,0x19,0xd9,0x1b,0xdb,0xda,0x1a,0x1e,0xde,0xdf,0x1f,0xdd,0x1d,0x1c,0xdc,
        0x14,0xd4,0xd5,0x15,0xd7,0x17,0x16,0xd6,0xd2,0x12,0x13,0xd3,0x11,0xd1,0xd0,0x10,
        0xf0,0x30,0x31,0xf1,0x33,0xf3,0xf2,0x32,0x36,0xf6,0xf7,0x37,0xf5,0x35,0x34,0xf4,
        0x3c,0xfc,0xfd,0x3d,0xff,0x3f,0x3e,0xfe,0xfa,0x3a,0x3b,0xfb,0x39,0xf9,0xf8,0x38,
        0x28,0xe8,0xe9,0x29,0xeb,0x2b,0x2a,0xea,0xee,0x2e,0x2f,0xef,0x2d,0xed,0xec,0x2c,
        0xe4,0x24,0x25,0xe5,0x27,0xe7,0xe6,0x26,0xe2,0x22,0xe3,0x23,0xe1,0x21,0x20,0xe0,
        0xa0,0x60,0x61,0xa1,0x63,0xa3,0xa2,0x62,0x66,0xa6,0xa7,0x67,0xa5,0x65,0x64,0xa4,
        0x6c,0xac,0xad,0x6d,0xaf,0x6f,0x6e,0xae,0xaa,0x6a,0x6b,0xab,0x69,0xa9,0xa8,0x68,
        0x78,0xb8,0xb9,0x79,0xbb,0x7b,0x7a,0xba,0xbe,0x7e,0x7f,0xbf,0x7d,0xbd,0xbc,0x7c,
        0xb4,0x74,0x75,0xb5,0x77,0xb7,0x76,0xb6,0x72,0xb2,0xb3,0x73,0xb1,0x71,0x70,0xb0,
        0x50,0x90,0x91,0x51,0x93,0x53,0x52,0x92,0x96,0x56,0x57,0x97,0x55,0x95,0x94,0x54,
        0x9c,0x5c,0x5d,0x9d,0x5f,0x9f,0x9e,0x5e,0x5a,0x9a,0x9b,0x5b,0x99,0x59,0x58,0x98,
        0x88,0x48,0x49,0x89,0x4b,0x8b,0x8a,0x4a,0x4e,0x8e,0x8f,0x4f,0x8d,0x4d,0x4c,0x8c,
        0x44,0x84,0x85,0x45,0x87,0x47,0x46,0x86,0x82,0x42,0x43,0x83,0x41,0x81,0x80,0x40
    ]);
}

/**
 * TLV 帧编码 (对齐 Go tlv.Encode + tlv_encode_option_with_buffer)
 * @param {number} tag      0x01~0x3F（低于 64），函数内部会自动加 0x80 (长长度) 和 0x40 (CRC)
 * @param {Uint8Array} data payload
 * @param {{checkCRC?:boolean, minLength?:number, maxLength?:number}} [opts]
 * @returns {Uint8Array} 完整 TLV 帧
 */
function _tlv_encode(tag, data, opts) {
    if (tag > 0x3F) throw new Error('tlv: tag out of range (must 0x01~0x3F)');
    const o = opts || {};
    const checkCRC  = (o.checkCRC !== false); // 默认启用 CRC（与 V3 服务器 Deserialize 兼容）
    const minLength = (typeof o.minLength === 'number') ? o.minLength : 1;
    const maxLength = (typeof o.maxLength === 'number') ? o.maxLength : 2;
    const effMin = Math.max(1, Math.min(4, minLength));
    const effMax = Math.max(effMin, Math.min(4, maxLength));
    const l = (data && data.length) | 0;

    let lengthSize = effMin;
    const maxValForMin = (() => {
        // 与 Go get_max_value_length 对齐：1B=0xFF, 2B=0xFFFF, 3B=0xFFFFFF, 4B=0xFFFFFFFF
        if (effMin === 1) return 0x000000FF;
        if (effMin === 2) return 0x0000FFFF;
        if (effMin === 3) return 0x00FFFFFF;
        return 0xFFFFFFFF;
    })();
    if (l > maxValForMin) lengthSize = effMax;

    let tagByte = tag & 0x3F;
    if (lengthSize !== effMin) tagByte |= 0x80;
    if (checkCRC) tagByte |= 0x40;

    const crcLen = checkCRC ? 2 : 0;
    const headerSize = 1 + lengthSize + crcLen;
    const out = new Uint8Array(headerSize + l);
    out[0] = tagByte;
    const dv = new DataView(out.buffer, out.byteOffset, out.byteLength);
    // 按 Go tlv_length_bytes: BE 写 uint32(opt.size) -> 取最后 lengthSize 字节
    // 即 lengthSize=1 时写 1B BE uint8，lengthSize=2 时写 2B BE uint16
    if (lengthSize === 1) {
        dv.setUint8(1, l & 0xff);
    } else if (lengthSize === 2) {
        dv.setUint16(1, l & 0xffff, false);
    } else if (lengthSize === 3) {
        out[1] = (l >>> 16) & 0xff;
        out[2] = (l >>> 8)  & 0xff;
        out[3] =  l         & 0xff;
    } else {
        dv.setUint32(1, l >>> 0, false);
    }
    // CRC
    if (checkCRC) {
        const crc = _tlv_calc_crc(data || new Uint8Array(0));
        out[1 + lengthSize    ] = crc[0];
        out[1 + lengthSize + 1] = crc[1];
    }
    // Value
    if (l > 0) out.set(data, headerSize);
    return out;
}

/**
 * TLV 帧解码 (对齐 Go tlv.tlv_decode_with_len)
 * @param {Uint8Array} b
 * @returns {{tag:number, value:Uint8Array, consumed:number}|null} 不是合法帧返回 null（不是抛错，便于后续回退为裸 FN 解析）
 */
function _tlv_try_decode(b) {
    if (!b || b.length < _TLV_HEADER_MIN_SIZE) return null;
    let tag = b[0];
    let lengthSize = 1;
    let checkCRC = false;
    if ((tag & 0x80) > 0) lengthSize = 2; // 默认 Min=1 Max=2；如果用户传特殊参数再升级，这里按 Go 默认（Min=1 Max=2）
    if ((tag & 0x40) > 0) checkCRC = true;
    tag &= 0x3F;
    const crcLen = checkCRC ? 2 : 0;
    const headerSize = 1 + lengthSize + crcLen;
    if (b.length < 1 + lengthSize) return null;
    const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
    let l;
    if (lengthSize === 1) l = dv.getUint8(1);
    else if (lengthSize === 2) l = dv.getUint16(1, false);
    else if (lengthSize === 3) l = ((b[1] & 0xff) << 16) | ((b[2] & 0xff) << 8) | (b[3] & 0xff);
    else l = dv.getUint32(1, false) >>> 0;
    if (b.length < headerSize + l) return null;
    const value = b.subarray(headerSize, headerSize + l);
    if (checkCRC) {
        const crc = b.subarray(headerSize - 2, headerSize);
        const calc = _tlv_calc_crc(value);
        if (crc[0] !== calc[0] || crc[1] !== calc[1]) return null; // 校验失败：非合法 TLV（或 CRC 错误）
    }
    return { tag, value, consumed: headerSize + l };
}


/* ============================================================
 *  内部工具：Base64 ↔ Uint8Array (兼容 Go 的 json.Marshal([]byte) 语义)
 *
 *  【关键说明：为什么这里要用顶层声明 + 「typeof 检测再赋值」，而不是
 *   套 IIFE 再直接写赋值？】
 *   · sock_rpc_v3 在浏览器下原生使用「独立 <script>」时：_u8ToB64 = xxx
 *     走"非严格模式下对未声明变量直接赋值 → window._u8ToB64"的隐式提升，
 *     脚本后续自己调用当然没问题。
 *   · 但 build.js 把所有 source 合并进同一个大 IIFE + 'use strict' →
 *     严格模式下 _u8ToB64 不声明直接赋值就是 ReferenceError。
 *   · 为了让 5 段源码"合并不动，单独加载也 OK"，我们只在 sock_rpc_v3
 *     开头加两行顶层 var 声明（单独 <script> 时就是全局 var，不影响），
 *     下面的 IIFE 检测 typeof===undefined 再赋值，两种加载方式都兼容。
 * ============================================================ */
var _u8ToB64;
var _b64ToU8;
var _fallbackBtoa;
var _fallbackAtob;
(function _ensureBase64Helpers() {
    if (typeof _u8ToB64 === 'undefined' || _u8ToB64 == null) {
        /**
         * Uint8Array → Base64 字符串（标准 UTF-8 → bytes → btoa 路径，长字节安全）
         * @param {Uint8Array} u8
         * @returns {string} Base64
         */
        _u8ToB64 = function (u8) {
            if (!u8 || u8.length === 0) return '';
            // 兼容性：Chrome/Firefox 的 TextDecoder.decode 可直接处理 Uint8Array
            // 为了避免 UTF-8 多字节字符出问题，走经典按字节 chunk 方案：
            let bin = '';
            const CHUNK = 0x8000; // 32768，避免 String.fromCharCode 栈爆
            for (let i = 0; i < u8.length; i += CHUNK) {
                const sub = u8.subarray(i, Math.min(i + CHUNK, u8.length));
                bin += String.fromCharCode.apply(null, Array.prototype.slice.call(sub));
            }
            return typeof btoa === 'function' ? btoa(bin) : _fallbackBtoa(bin);
        };
    }
    if (typeof _b64ToU8 === 'undefined' || _b64ToU8 == null) {
        /**
         * Base64 字符串 → Uint8Array（与 Go json.Unmarshal(..., *[]byte) 自动解码行为一致）
         * @param {string} b64
         * @returns {Uint8Array}
         */
        _b64ToU8 = function (b64) {
            if (!b64) return new Uint8Array(0);
            const bin = typeof atob === 'function' ? atob(b64) : _fallbackAtob(b64);
            const len = bin.length;
            const out = new Uint8Array(len);
            for (let i = 0; i < len; i++) out[i] = bin.charCodeAt(i);
            return out;
        };
    }
    // --- 兜底：极端老浏览器无 btoa/atob (几乎不会触发) ---
    function _fallbackBtoa(s) {
        const tab = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
        let out = '';
        let i = 0;
        while (i < s.length) {
            const c1 = s.charCodeAt(i++) & 0xff;
            const c2 = i < s.length ? s.charCodeAt(i++) & 0xff : NaN;
            const c3 = i < s.length ? s.charCodeAt(i++) & 0xff : NaN;
            const e1 = c1 >> 2;
            const e2 = ((c1 & 3) << 4) | (isNaN(c2) ? 0 : (c2 >> 4));
            const e3 = isNaN(c2) ? 64 : (((c2 & 15) << 2) | (isNaN(c3) ? 0 : (c3 >> 6)));
            const e4 = isNaN(c3) ? 64 : (c3 & 63);
            out += tab.charAt(e1) + tab.charAt(e2) + tab.charAt(e3) + tab.charAt(e4);
        }
        return out;
    }
    function _fallbackAtob(s) {
        s = String(s).replace(/\s/g, '').replace(/=+$/, '');
        const tab = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
        let out = '';
        let i = 0;
        while (i < s.length) {
            const e1 = tab.indexOf(s.charAt(i++));
            const e2 = tab.indexOf(s.charAt(i++));
            const e3 = (i < s.length) ? tab.indexOf(s.charAt(i++)) : 64;
            const e4 = (i < s.length) ? tab.indexOf(s.charAt(i++)) : 64;
            const c1 = (e1 << 2) | (e2 >> 4);
            const c2 = ((e2 & 15) << 4) | (e3 >> 2);
            const c3 = ((e3 & 3) << 6) | e4;
            out += String.fromCharCode(c1);
            if (e3 !== 64) out += String.fromCharCode(c2);
            if (e4 !== 64) out += String.fromCharCode(c3);
        }
        return out;
    }
})();

/* ============================================================
 *  内部工具：类型判断 (复用 V2 语义，独立实现避免依赖)
 * ============================================================ */
function _isFunction(v)  { return Object.prototype.toString.call(v) === '[object Function]'; }
function _isString(v)    { return Object.prototype.toString.call(v) === '[object String]'; }
function _isUint8Array(v){ return Object.prototype.toString.call(v) === '[object Uint8Array]'; }
function _isObject(v)    { return v !== null && Object.prototype.toString.call(v) === '[object Object]'; }

/* ============================================================
 *  SockRpcV3 主类
 * ============================================================ */
class SockRpcV3 {

    /* --------------------------------------------------------
     * 构造函数
     *  - new SockRpcV3("ws://host:port/ws")
     *  - new SockRpcV3({
     *        addr: "ws://...",
     *        timeoutMs: 10000,     // 单次 Call 超时 (ms)，对齐 Go ws_client.writeWait/readWait
     *        sliceSize: 512,       // 分片大小，必须 ∈ [1024 反直觉？不—Go Split 里 max(sliceSize,1024)，见下注释]
     *        autoSliceText: true,  // 是否用 TextMessage JSON 分片发送 (Go server 默认支持)
     *        onOpen / onClose / onMessage / onError,
     *    })
     *  注意：Go decoder/frame/utils.go#L14 有 `sliceSize = max(sliceSize, 1024)` 的下界，
     *       但 V2 sock_rpc_v2.js 历史默认 512，为避免用户旧代码迁移时行为突变，
     *       这里默认 512 但内部会自动传给 Go；若 Go 侧收到 <1024 则会 clamp 到 1024，
     *       浏览器侧接收按实际 T/I/S 数组合并，不会受影响。
     * -------------------------------------------------------- */
    constructor(option) {
        if (_isString(option)) option = { addr: option };
        const opts = option || {};
        this.addr       = opts.addr || '';
        this.timeoutMs  = opts.timeoutMs  || 10000;
        this.sliceSize  = opts.sliceSize  || _DEFAULT_SLICE_SIZE;
        this.autoSliceText = opts.autoSliceText !== false;
        // ---------- 断线重连策略（对齐 V2 SockRpc 的指数退避 + Stop 后禁止）----------
        //   - opts.autoReconnect:  开启后，close 事件如果不是 Stop()/manual close 就自动重试
        //   - opts.reconnectDelay: 首两次重试间隔 ms，默认 1000
        //   - opts.backoffMax:     最大间隔 ms，默认 20000；每次失败 *1.5 直到 cap 上限
        //   - opts.maxReconnectAttempts: 最大尝试次数，<=0 表示无限重试
        this.autoReconnect  = opts.autoReconnect !== false;
        this.reconnectDelay = (opts.reconnectDelay | 0) > 0 ? (opts.reconnectDelay | 0) : 1000;
        this.backoffMax     = (opts.backoffMax | 0)     > 0 ? (opts.backoffMax | 0)     : 20000;
        this.maxReconnectAttempts = typeof opts.maxReconnectAttempts === 'number' ? (opts.maxReconnectAttempts | 0) : 0;
        // 内部状态
        this._reconnectTimer    = null;     // setTimeout 句柄
        this._reconnectAttempts = 0;        // 连续失败次数
        this._currentDelay      = this.reconnectDelay;
        this._closing           = false;    // 正在执行用户主动 Stop()，此时不重连
        this._manualClose       = false;    // 下次 close 事件视为用户主动（Stop / Reconnect 前的关闭）

        // WebSocket 实例 + 状态
        this.sock      = null;
        this.connected = false;

        // 事件监听：onopen / onclose / onmessage / onerror (对齐 V2 Code 枚举)
        this.listeners = {
            onopen:    [],
            onclose:   [],
            onmessage: [],
            onerror:   [],
        };
        if (_isFunction(opts.onOpen))    this.OnOpen(opts.onOpen);
        if (_isFunction(opts.onClose))   this.OnClose(opts.onClose);
        if (_isFunction(opts.onMessage)) this.OnMessage(opts.onMessage);
        if (_isFunction(opts.onError))   this.OnError(opts.onError);

        // HTTP Header 模拟 (对应 Go client.Header map[string]string)
        // 每次 Call 会合并入 JsonCallObject.Header 字段
        this.header = {};

        // Bind 的本地服务对象：rpcObj[svr][mtd] = fn (对齐 V2 Bind)
        this.rpcObj = {};

        // 调用 ID 生成：单调递增 uint64 (对齐 Go id.NextId(1) 语义)
        // 【关键：真实 BigInt 可调用性检测】
        //   单纯 typeof BigInt==='function' 会被 installHook.js 等 mock 骗过去，
        //   最终 writeUint64BE 里 try BigInt(value) 抛错降级为 Number，然后
        //   Number(undefined) 变成 NaN → 误抛 FnError。这里用探针函数跑一轮
        //   完整 set/getBigUint64，成功才算真的支持。探测函数与 tools.js /
        //   fn.js 兜底 IIFE 三者保持一致。
        function __probeBigInt() {
            if (typeof BigInt !== "function") return false;
            try {
                const probe = BigInt(1);
                if (typeof DataView.prototype.setBigUint64 === "function") {
                    const buf = new ArrayBuffer(8);
                    const v   = new DataView(buf);
                    v.setBigUint64(0, probe, false);
                    const r = v.getBigUint64(0, false);
                    if (r === probe) return true;
                }
                return false;
            } catch (_e) { return false; }
        }
        // 优先复用 tools.js 在全局已经算好的 __probeBigInt 结果（避免重复探测）
        this._hasBigInt = (typeof __probeBigInt === 'function' && typeof window !== 'undefined' && typeof window.__probeBigIntResult === 'boolean')
            ? Boolean(window.__probeBigIntResult)
            : __probeBigInt();
        try { // 记住本次结果，fn/tools 再探测时能复用
            const root = (typeof window !== 'undefined' ? window : (typeof self !== 'undefined' ? self : (typeof globalThis!=='undefined'? globalThis : global)));
            if (root && typeof root.__probeBigIntResult !== 'boolean') root.__probeBigIntResult = this._hasBigInt;
        } catch(_e){/*ignore*/}
        this._nextId    = this._hasBigInt ? BigInt(1) : 1;

        // 分片片名生成：%02d 递增，过 99→0 (对齐 Go nrpc/wsocket/utils.go getSliceName)
        this._sliceCounter = 0;

        // 待响应的调用表：id(字符串/BigInt字符串化) → { okCb, errCb, resolve, reject, timer }
        this._pendingCalls = new Map();

        // 分片接收聚合缓存：片名 N → { total, size, chunks: Uint8Array[], received, createdAt }
        this._reassembly = new Map();

        // 分片缓存清理定时器 (每 60s 扫一次，扔掉 >60s 没收完的碎片)
        this._reapTimer = null;
    }

    /* ============================================================
     * 事件订阅（对齐 V2 SockRpc 同名 API）
     * ============================================================ */

    /** 监听 open 事件（连接就绪） */
    OnOpen(listener, options)    { this.AddEvent('onopen',    listener, options); }
    /** 监听 close 事件（连接断开） */
    OnClose(listener, options)   { this.AddEvent('onclose',   listener, options); }
    /** 监听 message 事件（广播 / 非 FN 原始消息 / 调试） */
    OnMessage(listener, options) { this.AddEvent('onmessage', listener, options); }
    /** 监听 error 事件 */
    OnError(listener, options)   { this.AddEvent('onerror',   listener, options); }

    /**
     * 通用事件注册（对齐 V2 SockRpc.AddEvent）
     * @param {'onopen'|'onclose'|'onmessage'|'onerror'} type
     * @param {Function} listener
     */
    AddEvent(type, listener /*, options*/) {
        const ls = this.listeners[type];
        if (!ls) return;
        // 去重
        const idx = ls.indexOf(listener);
        if (idx >= 0) return;
        ls.push(listener);
    }

    /**
     * 通用事件移除（对齐 V2 SockRpc.RemoveEvent）
     */
    RemoveEvent(type, listener /*, options*/) {
        const ls = this.listeners[type];
        if (!ls) return;
        const i = ls.indexOf(listener);
        if (i >= 0) ls.splice(i, 1);
    }

    /** 内部触发：把 evt 传给该类型的所有监听器 */
    _emit(type, evt) {
        const ls = this.listeners[type] || [];
        for (let i = 0; i < ls.length; i++) {
            try { ls[i].call(this, evt); } catch (e) { this._logErr(`listener[${type}] throw`, e); }
        }
    }

    /* ============================================================
     * 本地服务注册（对齐 V2 SockRpc.Bind）
     *   例：rpc.Bind("shop", { Test: (payload) => "helo" })
     *   服务器 method = "shop.Test" 反向 CALL 时会触发 obj.Test(...args)
     * ============================================================ */
    Bind(svr, obj) {
        if (!this.rpcObj[svr]) this.rpcObj[svr] = {};
        Object.assign(this.rpcObj[svr], obj || {});
    }

    /** 设置 Header（JsonCallObject.Header 会携带） */
    SetHeader(k, v) { this.header[k] = v; }

    /* ============================================================
     * 连接管理
     * ============================================================ */

    /**
     * 建立 WebSocket 连接（对齐 V2 SockRpc.Connect）
     * - 如果已存在 sock：返回现有 sock
     * - 如果此时已经有 _reconnectTimer（调度中）会先 cancel，避免并发 Connect
     * @param {{ready?: (ws:WebSocket)=>void, binaryType?: 'blob'|'arraybuffer'}} [option]
     * @returns {WebSocket|null}
     */
    Connect(option) {
        if (this.sock) return this.sock;
        // 取消先前的"重连调度"——显式 Connect() 代表用户主动连接，从 0 次重试开始
        if (this._reconnectTimer) { clearTimeout(this._reconnectTimer); this._reconnectTimer = null; }
        const opts = option || {};
        const binaryType = (opts.binaryType === 'arraybuffer' || opts.binaryType === 'blob')
            ? opts.binaryType
            : 'arraybuffer';
        try {
            const WebSocketCtor = (typeof WebSocket !== 'undefined') ? WebSocket
                               : (typeof window !== 'undefined' && window.WebSocket ? window.WebSocket : null);
            if (!WebSocketCtor) {
                this._logErr('WebSocket API not available');
                this._scheduleReconnect('WebSocket API missing');
                return null;
            }
            this.sock = new WebSocketCtor(this.addr);
            this.sock.binaryType = binaryType;

            // open
            this.sock.addEventListener('open', (evt) => {
                this.connected = true;
                // 连接成功：清空重连统计
                this._reconnectAttempts = 0;
                this._currentDelay      = this.reconnectDelay;
                this._manualClose       = false;
                if (_isFunction(opts.ready)) {
                    try { opts.ready.call(this, this.sock); } catch (e) { this._logErr('ready cb throw', e); }
                }
                this._emit('onopen', evt);
                this._startReapTimer();
            });

            // message (核心入口：对应 Go ws_server.go#L327 for{ReadMessage})
            this.sock.addEventListener('message', (evt) => this._onRawMessage(evt));

            // error（注意：浏览器 WS 会先 error 再 close；这里只转发，不做重连，留到 close 里判断）
            this.sock.addEventListener('error', (evt) => {
                this.connected = false;
                this._emit('onerror', evt);
            });

            // close：根据"是否主动 Stop/主动关闭"判断要不要自动重连
            this.sock.addEventListener('close', (evt) => {
                const wasManual  = this._manualClose || this._closing;
                const wasClosing = this._closing;
                this.connected   = false;
                this.sock        = null;
                this._stopReapTimer();
                this._rejectAllPending(new Error(`connection closed (code=${evt && evt.code ? evt.code : 'n/a'})`));
                this._reassembly.clear();
                // 一次性标志位：先把 manual 清掉，防止下一次连接又被误判
                this._manualClose = false;
                this._emit('onclose', evt);
                // Stop 后绝对不重连
                if (wasClosing) {
                    this._cancelReconnect('stopped by user');
                    return;
                }
                // 用户主动关闭本次连接（例如调用 Reconnect 前的 close）—— 重连计数不重置，交给 Reconnect 自己处理
                if (!wasManual && this.autoReconnect) {
                    this._scheduleReconnect(`close code=${evt && evt.code ? evt.code : 'n/a'}`);
                }
            });

            return this.sock;
        } catch (e) {
            this._logErr('Connect failed', e);
            this._emit('onerror', e);
            this._scheduleReconnect('exception during new WebSocket');
            return null;
        }
    }

    /** 关闭连接（对齐 V2 SockRpc.Stop）：标记 _closing=true，彻底禁止后续自动重连 + 清空监听器 */
    Stop() {
        this._closing = true;
        this._cancelReconnect('Stop() called');
        if (this.sock) {
            try { this.sock.close(); } catch (_) { /* ignore */ }
            this.sock = null;
        }
        this.connected = false;
        this._stopReapTimer();
        this._rejectAllPending(new Error('stopped by user'));
        this._reassembly.clear();
        // 清空监听器 (对齐 V2 Stop)
        this.listeners = { onopen: [], onclose: [], onmessage: [], onerror: [] };
    }

    /**
     * 主动重连（对应 index_v3.html L85 `rpc.Reconnect()` 处调用）：
     *  1) 先 Cancel 当前 pending 的重连定时器
     *  2) 如果当前仍保持连接：先标记"主动关闭本次"再 close，保证进入 close 回调但不触发新的自动重连
     *  3) 指数退避计数 + 尝试 Connect（立即或按 delay 调度）
     * @param {boolean} [immediate=true] true=立即尝试一次；false=按当前退避 delay 调度一次
     */
    Reconnect(immediate) {
        const shouldImmediate = immediate !== false;
        this._cancelReconnect('Reconnect() called');
        // 若当前仍连接，先主动 close（标记 manualClose，防止触发自动重连）
        if (this.sock) {
            this._manualClose = true;
            try { this.sock.close(); } catch (_) { /* ignore */ }
            this.sock = null;
            this.connected = false;
            this._rejectAllPending(new Error('reconnecting'));
            this._reassembly.clear();
            this._manualClose = false;
        }
        this._closing = false; // Stop 后也允许用 Reconnect() 恢复
        if (shouldImmediate) {
            // "手动"这次重试不消耗 attempts 计数？为避免死循环，仍增加一次，便于按 maxReconnectAttempts 停
            this._reconnectAttempts += 0; // 不增加：Reconnect 算用户主动触发的一次
            this.Connect();
        } else {
            this._scheduleReconnect('Reconnect(false) scheduled');
        }
    }

    /** 显式启用自动重连（也可构造时传入 autoReconnect=true） */
    EnableReconnect() { this.autoReconnect = true; }
    /** 显式禁用自动重连（也可构造时传入 autoReconnect=false） */
    DisableReconnect() {
        this.autoReconnect = false;
        this._cancelReconnect('DisableReconnect() called');
    }

    /**
     * 内部：调度一次"指数退避"重连
     *  - 达到 maxReconnectAttempts（>0）时停止
     *  - 否则延迟 Math.min(delay*1.5, backoffMax) 后 Connect()
     */
    _scheduleReconnect(reason) {
        if (this._closing) return;
        if (!this.autoReconnect) return;
        if (this._reconnectTimer) return;     // 已调度中就不叠加
        if (this.sock && this.connected) return; // 已在线无需重连
        this._reconnectAttempts++;
        const max = this.maxReconnectAttempts;
        if (max > 0 && this._reconnectAttempts > max) {
            this._logErr(`reconnect: stopped, attempts=${this._reconnectAttempts - 1} > maxReconnectAttempts=${max} (reason=${reason})`);
            return;
        }
        const delay = this._currentDelay;
        this._logErr(`reconnect: attempt=${this._reconnectAttempts} delay=${delay}ms (reason=${reason})`);
        this._emit('onreconnecting', {
            attempts: this._reconnectAttempts,
            delayMs:  delay,
            reason:   reason || '',
        });
        const self = this;
        this._reconnectTimer = setTimeout(() => {
            self._reconnectTimer = null;
            // 指数退避
            const next = Math.min(Math.floor(self._currentDelay * 1.5), self.backoffMax);
            if (!next || next <= self._currentDelay) self._currentDelay = self.backoffMax;
            else self._currentDelay = next;
            self.Connect();
        }, delay);
    }

    /** 内部：取消当前重连定时器（不清 attempts/delay 累计；Stop 时再清零） */
    _cancelReconnect(reason) {
        if (this._reconnectTimer) {
            clearTimeout(this._reconnectTimer);
            this._reconnectTimer = null;
            if (reason) this._emit('onreconnectcancelled', { reason });
        }
    }

    /* ============================================================
     * 顶层 API：Send / Notify / Call / CallPromise
     * ============================================================ */

    /**
     * 透传：不做任何封装，直接发送原始数据 (对齐 V2 SockRpc.Send)
     *   若尚未连接，会自动 Connect 后在 ready 回调内发送。
     * @param {string|Uint8Array|ArrayBuffer} raw
     */
    Send(raw) {
        const doSend = () => {
            try {
                if (!this.sock) throw new Error('ws not connected');
                this.sock.send(raw);
            } catch (e) { this._logErr('Send failed', e); }
        };
        if (!this.sock || !this.connected) {
            this.Connect({ ready: doSend });
        } else {
            doSend();
        }
    }

    /**
     * Notify：Fire-and-Forget（发送 ACTION_CALL 但不挂 pending）
     *  适用于不需要服务器回包的场景（登录通知 / 心跳）。
     * @param {string} method 方法名，如 "v1.Heartbeat"
     * @param  {...any} args   0..N 个参数（任意值：自动 AG.Encode；Uint8Array 走 Bytes 帧）
     */
    Notify(method, ...args) {
        const id = this._allocId();
        const fnFrame = this._buildCallFrame(id, method, args);
        if (!fnFrame) return;
        this._sendFnFrame(fnFrame);
    }

    /**
     * Call：发起一次 RPC 调用，带回调（对齐 V2 SockRpc.Call 签名，兼容 V2 老代码）。
     *
     * 【V2 兼容签名】
     *   rpc.Call(method, data, okCb, errCb, extra)
     *     → 等价于单参数：Call(method, data, okCb, errCb)  (extra 透传给 okCb/errCb 的第二参数，保持 V2 语义)
     * 【多参数签名（V3 原生）】
     *   rpc.Call(method, arg1, arg2, ..., okCb, errCb)
     *     → 末尾两个函数按顺序视为 okCb / errCb
     *
     * 每个 arg 的编码 (与 Go 保持一致):
     *   · 若 arg 本身是 Uint8Array：
     *       - 如果已经是合法 AG 帧（AG.IsArgument(arg)==true）→ 原样放入 JsonCallObject.Args[i]
     *       - 否则按 Bytes 类型先执行 AG.Encode(arg)（等价于 Go 的 `[]byte("sign") → ag.Encode → Args[][]byte`）
     *   · 其他类型 → 直接 AG.Encode(arg)
     *
     * okCb 收到的值：服务器 Reply(data) 的 data 是 AG 帧时自动 AG.Decode(data) 还原 JS 值；
     *   如果 data 不是合法 AG 帧（裸字节），原样返回 Uint8Array。
     * errCb 收到 Error 对象，message = 服务器返回错误字符串或超时。
     */
    Call(method /*, ...[args], [okCb], [errCb], [extra] */) {
        // V2 签名兼容：Call(method, data, okCb, errCb, extra)
        // V3 多参数：  Call(method, arg1, arg2, ..., okCb, errCb, extra)
        // 规则：从末尾反向剥离，顺序：
        //   1. 若末尾非函数 → 视为 extra（V2 的第 5 位参数，透传给回调的第二参数）
        //   2. 若末尾是函数 → 视为 errCb
        //   3. 若前一位还是函数 → 视为 okCb，之前被当作 errCb 的还原为 errCb
        //   4. 若只有 1 个函数 → 视为 okCb（errCb = undefined）
        //   剩下的全部作为 RPC 实参 args
        const all = Array.prototype.slice.call(arguments, 1);
        let realArgs = all.slice();
        let extra = undefined;
        let okCb  = undefined;
        let errCb = undefined;
        if (realArgs.length >= 1 && !_isFunction(realArgs[realArgs.length - 1])) {
            extra = realArgs.pop();
        }
        if (realArgs.length >= 1 && _isFunction(realArgs[realArgs.length - 1])) {
            errCb = realArgs.pop();
        }
        if (realArgs.length >= 1 && _isFunction(realArgs[realArgs.length - 1])) {
            okCb = realArgs.pop();
            // 保持顺序：okCb 在前，errCb 在后
            // 逆序 pop 时：第一次 pop 拿到的 errCb 实际就是末尾 errCb（正确），第二次 pop 拿到 okCb（正确）
            // 这里是对的，不用 swap
        } else if (errCb) {
            // 只拿到 1 个函数：是 okCb 而非 errCb
            okCb  = errCb;
            errCb = undefined;
        }

        const id = this._allocId();
        const fnFrame = this._buildCallFrame(id, method, realArgs);
        if (!fnFrame) {
            if (_isFunction(errCb)) errCb.call(null, new Error('build call frame failed'), extra, null);
            return;
        }

        // 挂 pending
        const timeoutMs = this.timeoutMs;
        const key = String(id);
        const timer = setTimeout(() => {
            const p = this._pendingCalls.get(key);
            if (!p) return;
            this._pendingCalls.delete(key);
            const err = new Error(`call ${method} timeout after ${timeoutMs}ms`);
            if (_isFunction(p.errCb)) {
                try { p.errCb.call(null, err, extra, null); } catch (e) { this._logErr('errCb throw', e); }
            }
            if (_isFunction(p.reject)) {
                try { p.reject(err); } catch (_) { /* ignore unhandled rejection catch by user */ }
            }
        }, timeoutMs);
        this._pendingCalls.set(key, {
            method,
            okCb: okCb  || undefined,
            errCb: errCb || undefined,
            extra,
            timer,
            resolve: undefined, // CallPromise 写入
            reject:  undefined,
        });

        // 发送
        try {
            this._sendFnFrame(fnFrame);
        } catch (e) {
            clearTimeout(timer);
            this._pendingCalls.delete(key);
            if (_isFunction(errCb)) {
                try { errCb.call(null, e, extra, null); } catch (_) { /* ignore */ }
            }
        }
    }

    /**
     * Promise 版 Call (现代 JS 友好，V3 新增)。
     * 用法：const result = await rpc.CallPromise("v1.Sign", new TextEncoder().encode("sign"));
     * 内部仍然挂同一个 _pendingCalls，自动填充 resolve/reject。
     */
    CallPromise(method, ...args) {
        const id = this._allocId();
        const fnFrame = this._buildCallFrame(id, method, args);
        if (!fnFrame) {
            return Promise.reject(new Error('build call frame failed'));
        }
        const self = this;
        return new Promise((resolve, reject) => {
            const key = String(id);
            const timer = setTimeout(() => {
                const p = self._pendingCalls.get(key);
                if (!p) return;
                self._pendingCalls.delete(key);
                reject(new Error(`call ${method} timeout after ${self.timeoutMs}ms`));
            }, self.timeoutMs);
            self._pendingCalls.set(key, {
                method,
                okCb: undefined,
                errCb: undefined,
                extra: undefined,
                timer,
                resolve,
                reject,
            });
            try {
                self._sendFnFrame(fnFrame);
            } catch (e) {
                clearTimeout(timer);
                self._pendingCalls.delete(key);
                reject(e);
            }
        });
    }

    /* ============================================================
     * 内部实现：ID / 片名分配
     * ============================================================ */

    /** 分配下一个单调递增的消息 ID（对齐 Go id.NextId(1)） */
    _allocId() {
        const cur = this._nextId;
        if (this._hasBigInt) {
            this._nextId = cur + BigInt(1);
        } else {
            this._nextId = (cur + 1) >>> 0; // uint32 回绕
        }
        return cur;
    }

    /** 下一个片名 (两位十进制 00-99 循环) */
    _nextSliceName() {
        const n = this._sliceCounter;
        this._sliceCounter = (this._sliceCounter + 1) % 100;
        return String(n).padStart(2, '0');
    }

    /* ============================================================
     * 内部实现：把 (id, method, args[]) 编码成 FN 帧 Uint8Array
     * ============================================================ */
    _buildCallFrame(id, method, argsArr) {
        // --- Step 1: 每个 arg → AG 编码 → Uint8Array (对齐 Go slots.Client 层 ag.Encode) ---
        const argsBytes = [];
        for (let i = 0; i < argsArr.length; i++) {
            const arg = argsArr[i];
            if (arg == null) {
                argsBytes.push(this._ag() ? this._ag().Encode(null) : new Uint8Array([0x3A, 0x70, 0x01, 0x00, 0x00]));
                continue;
            }
            if (_isUint8Array(arg)) {
                // 已经是 AG 帧？直接放入（允许用户自己预先编码复用）
                if (this._ag() && typeof this._ag().IsArgument === 'function' && this._ag().IsArgument(arg)) {
                    argsBytes.push(new Uint8Array(arg));
                } else {
                    // 普通 Uint8Array → 作为 Bytes 类型 AG.Encode
                    argsBytes.push(this._ag().Encode(arg));
                }
            } else {
                argsBytes.push(this._ag().Encode(arg));
            }
        }

        // --- Step 2: 组装 JsonCallObject (注意 []byte/[][]byte JSON 必须是 Base64) ---
        const jsonObj = {
            header: Object.assign({}, this.header || {}),
            method: String(method || ''),
        };
        if (argsBytes.length > 0) {
            jsonObj.args = argsBytes.map(u8 => _u8ToB64(u8));
        } else {
            // args 为空可以省略 (json:"...,omitempty")
        }
        // Go 结构体还包含 Data []byte / Error string，一般不用留空即可
        // （可选：把最后一个 args 当 Data？Go 里 Data 是 "hidden arg"，默认不用）

        // --- Step 3: JSON 序列化 → UTF-8 字节 ---
        let jsonBytes;
        try {
            jsonBytes = new TextEncoder().encode(JSON.stringify(jsonObj));
        } catch (e) {
            this._logErr('JSON.stringify JsonCallObject failed', e);
            return null;
        }

        // --- Step 4: fn.Encode(ACTION_CALL, id, jsonBytes) ---
        const fn = this._fn();
        if (!fn || !_isFunction(fn.Encode)) {
            this._logErr('window.Fn not loaded (did you <script src="fn.js">?)');
            return null;
        }
        const { buffer, error } = fn.Encode(ACTION_CALL, id, jsonBytes);
        if (error) { this._logErr('fn.Encode failed', error); return null; }
        return buffer;
    }

    /* ============================================================
     * 内部实现：发送 FN 帧 (自动分片，对齐 Go slicesTextSend)
     * ============================================================ */
    _sendFnFrame(fnBytes) {
        if (!this.sock) throw new Error('ws not connected');
        if (!fnBytes || fnBytes.length === 0) return;
        // ======================================================
        // 关键：FN 帧外层再包一层合法 TLV(CRC) 帧后才分片。
        // 理由：ws_server.go L359 先 `tlv.Deserialize(m)`，若 FN 帧首字节 0x40(字符 @)
        //       会被当成 tag=0x40 → bit6=1(CheckCRC=1) 但没 CRC → invalid crc，帧被丢弃不回包。
        //       因此统一包 tag=TLV_TYPE_BYTE(0x12), checkCRC=true 的 TLV 外层，
        //       服务端 Deserialize 会剥离 TLV 得到 Value(FN 帧)，再 fn.Action 就匹配。
        // ======================================================
        const tlvWrapped = _tlv_encode(_TLV_TYPE_BYTE, fnBytes, { checkCRC: true });
        const totalSize = tlvWrapped.length;
        const sliceSize = Math.max(1, this.sliceSize | 0);
        // 片名：每次发送分配一个新的（Go slicesTextSend 每次调用前外部 getSliceName）
        const name = this._nextSliceName();

        // 计算总分片数
        let totalSlice = Math.floor(totalSize / sliceSize);
        if (totalSize % sliceSize !== 0) totalSlice++;
        // Go Split 有 min/max clamp；这里 JS 直接切就行，不做限制

        for (let i = 0; i < totalSlice; i++) {
            const start = i * sliceSize;
            const end = Math.min(start + sliceSize, totalSize);
            const chunk = tlvWrapped.subarray(start, end);
            // TextMessage 分片 → JSON 对象，D 转 Base64
            // 注意：必须严格对齐 Go decoder/frame/slice.go#L16-L29 的 json tag（全部小写）:
            //   P byte   `json:"p"`
            //   N string `json:"n"`
            //   T byte   `json:"t"`
            //   I byte   `json:"i"`
            //   S uint32 `json:"s"`
            //   D []byte `json:"d"`  → JSON 序列化为 Base64 字符串
            const sliceObj = {
                p: _SLICE_TEXT,
                n: name,
                t: totalSlice, // byte：>255 片溢出？Go 版 T 是 byte，总分片数不能 >255
                i: i,
                s: totalSize,
                d: _u8ToB64(chunk), // 与 Go json.Marshal DataSlice{D:[]byte} 自动 Base64 一致
            };
            const text = JSON.stringify(sliceObj);
            // 以 WebSocket TextMessage 发送
            this.sock.send(text);
        }
    }

    /* ============================================================
     * 内部实现：接收入口 (对应 Go ws_server.go#L327 for{ReadMessage})
     * ============================================================ */
    _onRawMessage(evt) {
        const data = evt.data;
        try {
            // 区分 Text / Binary
            let slice;
            if (typeof data === 'string') {
                // TextMessage → JSON.parse → DataSlice（D 是 Base64 字符串）
                // 注意：Go 端 DataSlice 的 json tag 全部是小写（p/n/t/i/s/d），
                //       这里读小写为主；同时兼容大写以便调试或 V2 转发场景。
                try {
                    const obj = JSON.parse(data);
                    const P_ = (obj.p ?? obj.P) | 0;
                    const N_ = String(obj.n ?? obj.N ?? '');
                    const T_ = (obj.t ?? obj.T) | 0;
                    const I_ = (obj.i ?? obj.I) | 0;
                    const S_ = (obj.s ?? obj.S) | 0;
                    const D_ = _b64ToU8(obj.d ?? obj.D);
                    // 合法 DataSlice 必须满足：T≥1 且 I<T
                    if (T_ < 1 || I_ < 0 || I_ >= T_) {
                        throw new Error('invalid DataSlice bounds');
                    }
                    slice = { P: P_, N: N_, T: T_, I: I_, S: S_, D: D_ };
                } catch (_e1) {
                    // 不是合法 DataSlice JSON → 视为非分片原始文本消息
                    this._emit('onmessage', data);
                    return;
                }
            } else if (data instanceof ArrayBuffer || _isUint8Array(data)) {
                // BinaryMessage → 用 slice.js 的 Decode
                const buf = data instanceof ArrayBuffer ? new Uint8Array(data) : data;
                try {
                    if (window.DataSlice && typeof window.DataSlice === 'function' && typeof Decode === 'function') {
                        slice = Decode(buf);
                    } else {
                        // 没加载 slice.js → 当作完整非分片消息
                        this._maybeHandleFullFrame(buf);
                        return;
                    }
                } catch (_e2) {
                    this._maybeHandleFullFrame(buf);
                    return;
                }
            } else if (typeof Blob !== 'undefined' && data instanceof Blob) {
                // Blob → 转 ArrayBuffer 再处理 (异步)
                const self = this;
                const fr = new FileReader();
                fr.onload = function () { self._onRawMessage({ data: fr.result }); };
                fr.readAsArrayBuffer(data);
                return;
            } else {
                // 无法识别 → 直接 onmessage
                this._emit('onmessage', data);
                return;
            }

            // --- Step 1: 分片聚合 ---
            const full = this._reassemble(slice);
            if (!full) return; // 还没收齐

            // --- Step 2: 完整帧处理 (对应 ws_server.go#L359-L375) ---
            this._maybeHandleFullFrame(full);

        } catch (e) {
            this._logErr('_onRawMessage unexpected error', e);
        }
    }

    /** 分片聚合：收齐返回完整 Uint8Array，否则 null */
    _reassemble(slice) {
        const name = slice.N || '';
        const total = slice.T | 0;
        const idx   = slice.I | 0;
        const size  = slice.S | 0;
        const data  = _isUint8Array(slice.D) ? slice.D : new Uint8Array(0);

        // 单片直接返回
        if (total <= 1) {
            return data;
        }

        let ctx = this._reassembly.get(name);
        if (!ctx) {
            ctx = {
                total,
                size,
                chunks: new Array(total),
                received: 0,
                createdAt: Date.now(),
            };
            this._reassembly.set(name, ctx);
        }
        // 若收到的片已填充，跳过
        if (!ctx.chunks[idx]) {
            ctx.chunks[idx] = data;
            ctx.received += data.length;
        }

        // 收齐判断：长度达 size 且所有 chunk 已就位
        if (ctx.received >= ctx.size) {
            // 检查每个 chunk 是否存在
            let ok = true;
            for (let i = 0; i < ctx.total; i++) {
                if (!ctx.chunks[i]) { ok = false; break; }
            }
            if (ok) {
                // 拼接
                let out;
                if (ctx.total === 1) {
                    out = ctx.chunks[0];
                } else {
                    out = new Uint8Array(ctx.received);
                    let p = 0;
                    for (let i = 0; i < ctx.total; i++) {
                        const c = ctx.chunks[i];
                        out.set(c, p);
                        p += c.length;
                    }
                }
                this._reassembly.delete(name);
                return out;
            }
        }
        return null;
    }

    /**
     * 完整帧处理：对齐 Go ws_server.go#L359-L375 顺序
     *  1) tlv.Deserialize(m) 若成功 → m = tlvFrame.Value()
     *  2) fn.Action(m) 若成功 → HandleFn(按 Action 分发)
     *  3) 否则 handler.OnData → 本实现直接 onmessage 广播给用户监听器
     * @param {Uint8Array} fullBytes
     */
    _maybeHandleFullFrame(fullBytes) {
        let m = fullBytes;

        // Step 1: 可选 TLV 剥离（严格对齐 Go ws_server.go L359-L363 顺序）
        //   tlvFrame, err := tlv.Deserialize(m)
        //   if err == nil { m = tlvFrame.Value() }
        //   V3 这里用本文件内联的 _tlv_try_decode；回退支持 V2 sock_rpc_v2.js 的全局 NewTLVFromFrame
        const decoded = _tlv_try_decode(m);
        if (decoded && decoded.value && decoded.value.length > 0) {
            m = decoded.value;
        } else if (typeof NewTLVFromFrame === 'function') {
            try {
                const tlvFrame = NewTLVFromFrame(m);
                if (tlvFrame && typeof tlvFrame.Value === 'function') {
                    const v = tlvFrame.Value();
                    if (v && v.length) m = v;
                }
            } catch (_eTLV) { /* 不是 TLV 帧，忽略 */ }
        }

        // Step 2: fn.Action(m) 判断是否 FN 帧
        const fn = this._fn();
        if (fn && _isFunction(fn.Action)) {
            const { action, error } = fn.Action(m);
            if (!error && action !== ACTION_INVALID) {
                this._handleFn(m, action);
                return;
            }
        }

        // Step 3: 非 FN 帧 → 用户层 onmessage
        this._emit('onmessage', m);
    }

    /**
     * FN 帧分发（对齐 Go WsServer.HandleFn + 客户端 channel_client SendData 回包处理）
     * 分 4 个 Action 分支：
     *   · REPLY_SUCCESS(0x02) → pending okCb / Promise.resolve
     *   · REPLY_ERROR (0x03) → pending errCb / Promise.reject
     *   · CALL        (0x01) → 服务器调用客户端 Bind 服务
     *   · BROADCAST   (0xFF) → 广播 onmessage
     */
    _handleFn(fnBytes, action) {
        const fn = this._fn();
        const id   = fn.Id(fnBytes);
        const body = fn.Data(fnBytes) || new Uint8Array(0);

        switch (action) {
            case ACTION_REPLY_SUCCESS: {
                const key = String(id);
                const p = this._pendingCalls.get(key);
                if (!p) return; // 可能超时已删 / Notify 的无等待
                this._pendingCalls.delete(key);
                clearTimeout(p.timer);
                // 自动 AG 解码
                const val = this._tryAgDecode(body);
                if (_isFunction(p.okCb)) {
                    try { p.okCb.call(null, val, p.extra, fnBytes); } catch (e) { this._logErr('okCb throw', e); }
                }
                if (_isFunction(p.resolve)) {
                    try { p.resolve(val); } catch (_) { /* ignore user unhandled */ }
                }
                break;
            }
            case ACTION_REPLY_ERROR: {
                const key = String(id);
                const p = this._pendingCalls.get(key);
                if (!p) return;
                this._pendingCalls.delete(key);
                clearTimeout(p.timer);
                const msg = (body && body.length) ? new TextDecoder().decode(body) : 'unknown rpc error';
                const err = new Error(msg);
                if (_isFunction(p.errCb)) {
                    try { p.errCb.call(null, err, p.extra, fnBytes); } catch (e) { this._logErr('errCb throw', e); }
                }
                if (_isFunction(p.reject)) {
                    try { p.reject(err); } catch (_) { /* ignore user unhandled */ }
                }
                break;
            }
            case ACTION_CALL: {
                // 服务器反向调用客户端方法 (对应 Go 客户端的 OnBind + 服务端 Call 客户端)
                try {
                    this._handleIncomingCall(id, body);
                } catch (e) {
                    this._logErr('_handleIncomingCall throw', e);
                    // 回一个错误给服务器
                    this._replyError(id, e.message || String(e));
                }
                break;
            }
            case ACTION_BROADCAST: {
                // 广播：自动 AG 解码
                const val = this._tryAgDecode(body);
                this._emit('onmessage', val);
                break;
            }
            default:
                // 未知 Action → 按原样 onmessage
                this._emit('onmessage', fnBytes);
        }
    }

    /**
     * 处理服务器反向发起的 ACTION_CALL (对应客户端 Bind 的本地服务)
     * 流程：
     *   body (UTF-8 JSON) → JsonCallObject → method 切 "svr.mtd"
     *                      → Args[i] 每个 Base64→Uint8Array→AG.Decode→JS 值
     *                      → rpcObj[svr][mtd](...args)
     *                      → return 或 throw
     *                      → 给服务器回 ACTION_REPLY_SUCCESS / REPLY_ERROR
     */
    _handleIncomingCall(id, body) {
        const jsonStr = new TextDecoder().decode(body || new Uint8Array(0));
        let callObj;
        try {
            callObj = JSON.parse(jsonStr);
        } catch (e) {
            this._replyError(id, `invalid JsonCallObject JSON: ${e.message}`);
            return;
        }
        const method = String(callObj.method || '');
        // 切分 svr.mtd
        const dot = method.indexOf('.');
        if (dot <= 0) { this._replyError(id, `invalid method: ${method}`); return; }
        const svr = method.substring(0, dot);
        const mtd = method.substring(dot + 1);
        const svrObj = this.rpcObj && this.rpcObj[svr];
        const fn = (svrObj && _isFunction(svrObj[mtd])) ? svrObj[mtd] : null;
        if (!fn) {
            this._replyError(id, `client service not bound: ${svr}.${mtd}`);
            return;
        }
        // Args 解码
        const args64 = Array.isArray(callObj.args) ? callObj.args : [];
        const args = [];
        for (let i = 0; i < args64.length; i++) {
            const u8 = _b64ToU8(args64[i]); // Base64→Uint8Array (是 AG 帧)
            args.push(this._tryAgDecode(u8));
        }

        // 调用（同步 / Promise 兼容）
        let ret;
        try {
            ret = fn.apply(svrObj, args);
        } catch (e) {
            this._replyError(id, e.message || String(e));
            return;
        }
        // 支持返回 Promise
        const self = this;
        const sendOk = (resultVal) => {
            // ========================================================
            // 【关键约定（对齐 V2 sock_rpc_v2.js + Go handler 返回值语义）】
            //   AG 协议只用于【函数参数】的编码/解码（服务端 → 客户端 call 的 args，
            //   以及客户端 → 服务端 Call 的 args）；【返回值】不包 AG，而是：
            //     · Uint8Array / Buffer 类字节 → 原样当作 fn.Reply Data 写入（对应 Go 端的 []byte 返回）
            //     · string                       → TextEncoder().encode(s)（对应 Go 端 string 返回，服务端 string(data)）
            //     · number / boolean / bigint    → 先 toString() 再转字节 (兼容最基本的标量返回)
            //     · null / undefined             → 0 字节 Data
            //     · object / array（非字节）     → JSON.stringify 后 UTF-8（服务端可 json.Unmarshal）
            //
            //  为什么不对返回值再 ag.Encode？——
            //     用户实测：shop.Test 返回 "helo178..."，V3 发出去后服务端收到 ":phelo178..."，
            //     前面多了 AG Magic `:p`（TLV_TYPE_BYTES 的 :p 帧头），导致服务端 string(data)
            //     直接读出协议头，污染真实数据。AG 约定只在函数 Args 层生效。
            // ========================================================
            let bytes;
            try {
                if (resultVal == null) {
                    bytes = new Uint8Array(0);
                } else if (_isUint8Array(resultVal)) {
                    bytes = resultVal;
                } else if (resultVal instanceof ArrayBuffer) {
                    bytes = new Uint8Array(resultVal);
                } else if (Array.isArray(resultVal)) {
                    // 字节数组？
                    let maybeBytes = true;
                    const n = resultVal.length | 0;
                    const copy = new Uint8Array(n);
                    for (let i = 0; i < n; i++) {
                        const v = Number(resultVal[i]);
                        if (Number.isNaN(v)) { maybeBytes = false; break; }
                        const byte = (v | 0) & 0xFF;
                        if (byte !== (v < 0 ? 255 - Math.abs(v) - 1 : v) && v > 0xFF) { maybeBytes = false; break; }
                        copy[i] = byte;
                    }
                    if (maybeBytes) {
                        bytes = copy;
                    } else {
                        bytes = new TextEncoder().encode(JSON.stringify(resultVal));
                    }
                } else if (_isString(resultVal)) {
                    bytes = new TextEncoder().encode(resultVal);
                } else if (typeof resultVal === 'boolean') {
                    bytes = new TextEncoder().encode(resultVal ? 'true' : 'false');
                } else if (typeof resultVal === 'number' || typeof resultVal === 'bigint') {
                    bytes = new TextEncoder().encode(String(resultVal));
                } else if (_isObject(resultVal)) {
                    bytes = new TextEncoder().encode(JSON.stringify(resultVal));
                } else {
                    // 兜底：转字符串
                    bytes = new TextEncoder().encode(String(resultVal));
                }
            } catch (e) {
                self._replyError(id, `encode reply failed: ${e.message}`);
                return;
            }
            self._replySuccess(id, bytes);
        };
        if (ret && _isFunction(ret.then)) {
            ret.then(sendOk, (e) => this._replyError(id, e && e.message ? e.message : String(e)));
        } else {
            sendOk(ret);
        }
    }

    /** 给服务器回一个 ACTION_REPLY_SUCCESS (id, data[]byte) */
    _replySuccess(id, dataU8) {
        const fn = this._fn();
        if (!fn) return;
        const { buffer, error } = fn.Encode(ACTION_REPLY_SUCCESS, id, dataU8 || new Uint8Array(0));
        if (error) { this._logErr('fn.Encode reply success failed', error); return; }
        try { this._sendFnFrame(buffer); } catch (e) { this._logErr('send reply failed', e); }
    }
    /** 给服务器回一个 ACTION_REPLY_ERROR (id, errMsg) */
    _replyError(id, errMsg) {
        const fn = this._fn();
        if (!fn) return;
        const body = new TextEncoder().encode(String(errMsg || ''));
        const { buffer, error } = fn.Encode(ACTION_REPLY_ERROR, id, body);
        if (error) { this._logErr('fn.Encode reply error failed', error); return; }
        try { this._sendFnFrame(buffer); } catch (e) { /* ignore */ }
    }

    /* ============================================================
     * 内部实现：AG 解码兜底 (非 AG 帧原样返回 Uint8Array)
     * ============================================================ */
    _tryAgDecode(u8) {
        const ag = this._ag();
        if (ag && _isFunction(ag.IsArgument) && ag.IsArgument(u8) && _isFunction(ag.Decode)) {
            try { return ag.Decode(u8); } catch (_) { /* 解析失败就返回原字节 */ }
        }
        return u8;
    }

    /* ============================================================
     * 内部实现：挂在全局的 AG / Fn 引用（避免打包器下 window.* 不可达）
     * ============================================================ */
    _ag() {
        const w = (typeof window !== 'undefined') ? window : (typeof self !== 'undefined' ? self : globalThis);
        return w.AG || null;
    }
    _fn() {
        const w = (typeof window !== 'undefined') ? window : (typeof self !== 'undefined' ? self : globalThis);
        return w.Fn || null;
    }

    /* ============================================================
     * 内部实现：清理
     * ============================================================ */
    _rejectAllPending(err) {
        if (!this._pendingCalls || this._pendingCalls.size === 0) return;
        for (const [key, p] of this._pendingCalls.entries()) {
            clearTimeout(p.timer);
            if (_isFunction(p.errCb)) {
                try { p.errCb.call(null, err, p.extra, null); } catch (_) { /* ignore */ }
            }
            if (_isFunction(p.reject)) {
                try { p.reject(err); } catch (_) { /* ignore */ }
            }
        }
        this._pendingCalls.clear();
    }

    _startReapTimer() {
        if (this._reapTimer) return;
        const self = this;
        this._reapTimer = setInterval(() => {
            const now = Date.now();
            const MAX_AGE = 60000;
            for (const [name, ctx] of self._reassembly.entries()) {
                if (now - ctx.createdAt > MAX_AGE) {
                    self._reassembly.delete(name);
                }
            }
        }, 60000);
    }
    _stopReapTimer() {
        if (this._reapTimer) { clearInterval(this._reapTimer); this._reapTimer = null; }
    }

    _logErr(msg, cause) {
        if (typeof console === 'undefined' || !console.error) return;
        console.error('[SockRpcV3]', msg, cause || '');
    }
}

/* ============================================================
 * 导出：浏览器全局 / CommonJS / ESModule 兼容
 *
 * 额外暴露 3 个 Go tlv 包等价的顶层工具（对齐 examples/ws/node/main.go L84 / examples/ws/main.go L152）：
 *   · TlvValue(data)   => tlv.Value(d)   先剥外层 TLV 拿 Value，失败则返回原字节（不抛错）
 *   · TlvValueAsText   => tlv.Value(d) 再 UTF-8 解码（最常用：服务端 tlv.Json 后客户端 string(data)）
 *   · TlvValueAsJson   => tlv.Value(d) 再 JSON.parse
 * ============================================================ */
(function _exports() {
    /**
     * 等价 Go tlv.Value(d []byte, opts...) []byte：
     *   1) 先尝试用 _tlv_decode_opt 解析输入为合法 TLV（带/不带 CRC 都兼容）
     *   2) 解析成功 => 返回 TLV 的 Value 段
     *   3) 解析失败 / 入参非法 => 原样返回输入（和 Go 完全一致，不会抛错）
     *
     * 典型场景：v1.Sign 返回 tlv.Json(auth) 编码后的 []byte
     *           浏览器端拿到 Uint8Array 后先 TlvValue(...) 再 JSON.parse / TextDecoder
     *
     * @param {Uint8Array|ArrayBuffer|number[]|null|undefined} data
     * @returns {Uint8Array}
     */
    function TlvValue(data) {
        // - 空值 / 非字节 直接 return 空 Uint8Array
        if (data == null) return new Uint8Array(0);
        let u8 = data;
        if (data instanceof ArrayBuffer) u8 = new Uint8Array(data);
        else if (Array.isArray(data)) u8 = new Uint8Array(data);
        else if (!(u8 instanceof Uint8Array)) {
            try {
                const n = Number(u8.length | 0);
                const copy = new Uint8Array(n);
                for (let i = 0; i < n; i++) copy[i] = u8[i] | 0;
                u8 = copy;
            } catch (_eCast) {
                return new Uint8Array(0);
            }
        }
        // - 空 buffer 直接返回
        if (u8.length < _TLV_HEADER_MIN_SIZE) return u8;
        const r = _tlv_try_decode(u8);
        // - 失败：返回入参本身（保持字节类型一致）；成功：返回 value
        if (!r || !r.value) return u8;
        return r.value;
    }

    /**
     * TlvValue 后按 UTF-8 解码成 string（对齐 Go fmt.Println(string(tlv.Value(data)))）
     * 非 TLV 或 TLV value 非法时返回原字节的 UTF-8（等价 Go 行为，不会抛错）
     */
    function TlvValueAsText(data) {
        const v = TlvValue(data);
        try { return new TextDecoder('utf-8').decode(v); }
        catch (_e) {
            try { return new TextDecoder().decode(v); }
            catch (_e2) { return ''; }
        }
    }

    /**
     * TlvValue 后按 JSON.parse 成对象；失败抛错（由调用方 try/catch）
     * 对应服务端 tlv.Json(any) 的对称解码。
     */
    function TlvValueAsJson(data) {
        const text = TlvValueAsText(data);
        if (text === '' || text == null) return null;
        return JSON.parse(text);
    }

    // - 挂到 class 上作为静态方法，也作为独立导出（用户 index_v3 里可直接 TlvValue(...)）
    SockRpcV3.TlvValue       = TlvValue;
    SockRpcV3.TlvValueAsText = TlvValueAsText;
    SockRpcV3.TlvValueAsJson = TlvValueAsJson;

    /**
     * 等价 Go tlv.Json(v any, opts ...FrameOption) []byte（对齐 examples/ws/main.go L152 `return tlv.Json(auth), nil`）
     *   1) JSON.stringify(v) —— 失败抛错（调用方 try/catch）
     *   2) 用 UTF-8 编码 JSON 字符串 → Uint8Array
     *   3) 调用 _tlv_encode(TLV_TYPE_JSON, jsonData, {checkCRC: true}) 包一层 JSON-TLV（默认 CRC=true 长度自动 1/2B）
     *   4) 返回 Uint8Array（和 Go 的 []byte 一一对应）
     *
     * 【对称性】：
     *   - 返回字节 → 送入 rpc.Call(...) 的 args → 服务端 handler 收到 []byte
     *     -> 调用方 Go 侧：`tlv.Value(d)` 剥离外层得到 JSON 字节，再 json.Unmarshal(&target)
     *     -> 调用方 JS 侧：本方法返回字节，服务端 handler 直接用 tlv.Value(d)+json.Unmarshal
     *
     * @param {any} obj
     * @param {{checkCRC?:boolean,minLength?:number,maxLength?:number}} [opts]
     * @returns {Uint8Array}
     */
    function TlvJson(obj, opts) {
        const text = JSON.stringify(obj);
        let bytes;
        if (typeof TextEncoder === 'function') {
            bytes = new TextEncoder().encode(text);
        } else {
            // - 回退：手动 UTF-8 encode（兼容老环境）
            bytes = new Uint8Array(text.length * 3);
            let pos = 0;
            for (let i = 0; i < text.length; i++) {
                let c = text.charCodeAt(i);
                if (c < 0x80) {
                    bytes[pos++] = c;
                } else if (c < 0x800) {
                    bytes[pos++] = 0xC0 | (c >> 6);
                    bytes[pos++] = 0x80 | (c & 0x3F);
                } else if (c >= 0xD800 && c <= 0xDBFF && i + 1 < text.length) {
                    const c2 = text.charCodeAt(++i);
                    if (c2 >= 0xDC00 && c2 <= 0xDFFF) {
                        c = 0x10000 + ((c & 0x3FF) << 10) + (c2 & 0x3FF);
                        bytes[pos++] = 0xF0 | (c >> 18);
                        bytes[pos++] = 0x80 | ((c >> 12) & 0x3F);
                        bytes[pos++] = 0x80 | ((c >> 6) & 0x3F);
                        bytes[pos++] = 0x80 | (c & 0x3F);
                    } else { --i; bytes[pos++] = 0xEF; bytes[pos++] = 0xBF; bytes[pos++] = 0xBD; }
                } else {
                    bytes[pos++] = 0xE0 | (c >> 12);
                    bytes[pos++] = 0x80 | ((c >> 6) & 0x3F);
                    bytes[pos++] = 0x80 | (c & 0x3F);
                }
            }
            bytes = bytes.subarray(0, pos);
        }
        return _tlv_encode(_TLV_TYPE_JSON, bytes, Object.assign({ checkCRC: true }, opts || {}));
    }

    SockRpcV3.TlvJson = TlvJson;

    const _bag = {
        SockRpcV3,
        ACTION_CALL, ACTION_REPLY_SUCCESS, ACTION_REPLY_ERROR, ACTION_INVALID, ACTION_BROADCAST,
        TlvValue, TlvValueAsText, TlvValueAsJson, TlvJson,
    };
    const root = (typeof window !== 'undefined') ? window :
                 (typeof self   !== 'undefined') ? self   :
                 (typeof global !== 'undefined') ? global : globalThis;
    Object.assign(root, _bag);

    // ============================================================
    //  兼容：把 fn.js 导出的 Fn.* 也再提一份裸名到全局（对齐 build.js 白名单）
    //  这样用户既可以 `SockRpcV3.xxx`，也可以裸 `BuildCall(...)`。
    //  · fn.js 原本在浏览器下只挂到 window.Fn = { BuildCall, IsFn, ParseData, ... }
    //  · 如果加载顺序乱了 / 用 bundle 合并后 fn.js 比这段早执行 → root.Fn 就是对象
    //  · 如果真的没加载 fn.js → 这里静默跳过（避免 throw）
    // ============================================================
    try {
        const Fn = (root && typeof root.Fn === 'object') ? root.Fn : null;
        if (Fn) {
            const ALIASES = [
                ['BuildCall',       'BuildCall'],
                ['BuildReply',      'BuildReply'],
                ['BuildBroadcast',  'BuildBroadcast'],
                ['ParseData',       'ParseData'],
                ['IsFn',            'IsFn'],
                ['Encode',          'Encode'],
                ['Decode',          'Decode'],
                ['Action',          'Action'],
                ['Id',              'Id'],
                ['Data',            'Data'],
                ['HeaderSize',      'FN_HEADER_SIZE'],
                ['HeaderSize',      'HeaderSize'],
            ];
            for (const [fnKey, alias] of ALIASES) {
                if (typeof Fn[fnKey] === 'function' || typeof Fn[fnKey] === 'number') {
                    if (typeof root[alias] === 'undefined') root[alias] = Fn[fnKey];
                }
            }
            // MAGIC / HEADER SIZE 常量也提一下
            if (typeof Fn.Magic1 !== 'undefined' && typeof root.FN_MAGIC_1 === 'undefined') root.FN_MAGIC_1 = Fn.Magic1;
            if (typeof Fn.Magic2 !== 'undefined' && typeof root.FN_MAGIC_2 === 'undefined') root.FN_MAGIC_2 = Fn.Magic2;
            if (typeof root.FN === 'undefined') root.FN = Fn;
        }
    } catch (_e) { /* 任何异常静默，不影响主模块导出 */ }

    if (typeof module !== 'undefined' && module.exports) module.exports = _bag;
})();
