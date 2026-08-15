/*!
 * sloth_v3_bundle.js  /  sloth_v3_min.js
 *
 * Concatenated & built from:
 *   1. tools.js
 *   2. slice.js
 *   3. ag.js
 *   4. fn.js
 *   5. sock_rpc_v3.js
 *
 * Build order exactly matches: examples/ws/web/index_v3.html L17-L21
 * Generated at: 2026-08-15T08:02:41.539Z
 */
(function () {
"use strict";

/* ============================================================
 * source: tools.js
 * ============================================================ */
/* ============================================================
 *  工具函数：BigInt 支持检测（真实可调用性检测）
 *
 *  为什么要"真实调用 1 次"检测，而不是 typeof BigInt==='function'？
 *  用户环境里常见 installHook.js / Chrome DevTools 插件 / 某些
 *  JS 沙箱会 mock BigInt = function() { throw new Error(...) }，
 *  或 Object.defineProperty(window,'BigInt') 给出假的构造器。
 *  单纯 typeof 会判断为可用，然后 writeUint64BE 的 BigInt(value)
 *  抛错，降级到 Number 分支但 Number(value) 对某些值又不够，
 *  最终抛出："fn: invalid uint64 value (no BigInt support in this environment)"
 *  误导用户以为环境没 BigInt，实际上只是 mock 的 BigInt 不可用。
 *
 *  这里真实跑一次 BigInt(1) 成功才算 _HAS_BIGINT = true；失败就
 *  一律 Number 降级（保证不抛错）。同样检测也给 sock_rpc_v3.js
 *  和 fn.js 的兜底 IIFE 复用，三者保持一致。
 * ============================================================ */

function __probeBigInt() {
    if (typeof BigInt !== "function") return false;
    try {
        const probe = BigInt(1);
        if (typeof DataView.prototype.setBigUint64 === "function") {
            const buf = new ArrayBuffer(8);
            const v   = new DataView(buf);
            v.setBigUint64(0, probe, false);
            const r = v.getBigUint64(0, false);
            if (r === probe) return true; // 读回一致才认为完整支持
        }
        return false;
    } catch (_e) { return false; }
}
const _HAS_BIGINT = __probeBigInt();


function encode_bytes(v) {
  try {
    return new TextEncoder().encode(JSON.stringify(v));
  } catch (err) {
    return new Uint8Array(0);
  }
}

function decode_bytes(b) {
  try {
    return JSON.parse(new TextDecoder().decode(b));
  } catch (err) {
    return null;
  }
}

function decode_string(b) {
  try {
    return new TextDecoder().decode(b);
  } catch (err) {
    return null;
  }
}

/**
 * 将数字转换为 BigInt（若支持），否则退化为 Number
 * @param {number|string|bigint} n
 * @returns {bigint|number}
 */
function _toBig(n) {
  return _HAS_BIGINT ? BigInt(n) : Number(n);
}

/**
 * 两个 BigInt/Number 的位与运算
 */
function _bitAnd(a, b) {
  if (_HAS_BIGINT) return a & BigInt(b);
  return a & b;
}

/**
 * 左移
 */
function _lshift(a, n) {
  if (_HAS_BIGINT) return a << BigInt(n);
  // Number 左移超 32 位会出问题，这里用乘法兜底
  if (n < 31) return a << n;
  return a * Math.pow(2, n);
}

/**
 * 右移（逻辑/算术统一用 BigInt >>> 对无符号，对有符号用 BigInt >>）
 */
function _urshift(a, n) {
  if (_HAS_BIGINT) return a >> BigInt(n);
  if (n < 31) return a >>> n;
  return Math.floor(a / Math.pow(2, n));
}

/* ============================================================
 *  辅助函数：字节处理
 * ============================================================ */

/**
 * 将字节数组零扩展/截断到 n 字节（大端语义：高位在左）
 *   - 长度 >= n：取最右侧 n 字节（低位）
 *   - 长度 <  n：左侧补 0
 * @param {Uint8Array} b
 * @param {number} n
 * @returns {Uint8Array}
 */
function zeroExtendN(b, n) {
  const l = b.length;
  const out = new Uint8Array(n);
  if (l >= n) {
    out.set(b.subarray(l - n, l), 0);
    return out;
  }
  out.set(b, n - l);
  return out;
}

/** 零扩展到 2 字节 */
function zeroExtend2byte(b) {
  return zeroExtendN(b, 2);
}
/** 零扩展到 4 字节 */
function zeroExtend4byte(b) {
  return zeroExtendN(b, 4);
}
/** 零扩展到 8 字节 */
function zeroExtend8byte(b) {
  return zeroExtendN(b, 8);
}

/**
 * int64 → 大端字节数组（简易压缩：去除前导 0，但不 trim 负数的 FF 高位）
 *   [0 0 0 0 0 0 128 0] → [128,0]
 *   [0 0 0 0 0 0 1 1]   → [1,1]
 *   [0 0 0 0 0 0 0 0]   → [0]
 *   [FF FF FF FF FF FF FF FF] (-1) → 保留 8 字节（首字节是 FF 不能 trim）
 * @param {bigint|number} i
 * @returns {Uint8Array}
 */
function int_to_byte(i) {
  const bi = _toBig(i);
  // 写入 8 字节 BigEndian
  const b = new Uint8Array(8);
  const mask = _toBig(0xff);
  for (let idx = 7; idx >= 0; idx--) {
    b[idx] = Number(_bitAnd(bi, 0xff));
    // 注意：无符号右移 8 位
    if (_HAS_BIGINT) {
      // BigInt 右移对负数是算术右移，正好取补码高位
      bi_holder(bi >> BigInt(8));
    } else {
      // Number：手动模拟补码右移
      bi_holder(Math.floor(Number(bi) / 256));
    }
  }
  // -- 下面重新实现（因上面 bi 不可变） --
  return _intToByteImpl(_toBig(i));
}

/**
 * holder 占位，实际使用闭包变量实现
 * @ignore
 */
function bi_holder(_v) {
  /* no-op, replaced by closure in real impl */
}

/**
 * int64 → 大端字节数组 + 压缩（真实实现）
 * @param {bigint|number} val
 * @returns {Uint8Array}
 */
function _intToByteImpl(val) {
  const b = new Uint8Array(8);
  let v = val;
  const ff = _toBig(0xff);
  for (let idx = 7; idx >= 0; idx--) {
    b[idx] = Number(_bitAnd(v, 0xff));
    if (_HAS_BIGINT) {
      v = v >> BigInt(8);
    } else {
      // Number 模拟算术右移 8 位（处理负数补码）
      if (v >= 0) {
        v = Math.floor(v / 256);
      } else {
        v = Math.ceil(v / 256);
      }
    }
  }
  // 简易压缩：从前向后找第一个不必保留的字节
  // 规则：
  //   - 如果首字节 b[0] 高位是 0（正数），则可以 trim 掉连续的前导 0 字节（但至少保留 1B）
  //   - 如果首字节高位是 1（负数），则不能 trim（保持符号扩展）
  let pos = 0;
  const isPositive = (b[0] & 0x80) === 0;
  if (isPositive) {
    while (pos < 7 && b[pos] === 0) {
      pos++;
    }
  }
  if (pos === 0) {
    return b;
  }
  const outLen = 8 - pos;
  const out = new Uint8Array(outLen);
  out.set(b.subarray(pos, 8), 0);
  return out;
}

/** uint64 → 字节数组（复用 int_to_byte，补码一致） */
function uint_to_byte(u) {
  return _intToByteImpl(_toBig(u));
}

/**
 * 字节数组（大端） → uint64（Go 版逻辑：先无符号解析）
 *   Go 版 to_int64 = int64(binary.BigEndian.Uint64(zeroExtend8byte(b)))
 *   这里 to_int64 复用 to_uint64 的位模式，通过窄化函数得到有符号语义
 * @param {Uint8Array} b
 * @returns {bigint|number}
 */
function to_uint64(b) {
  const padded = zeroExtend8byte(b);
  const dv = new DataView(padded.buffer, padded.byteOffset, padded.byteLength);
  if (_HAS_BIGINT && typeof dv.getBigUint64 === "function") {
    return dv.getBigUint64(0, false); // big-endian 无符号
  }
  const hi =
    ((padded[0] << 24) | (padded[1] << 16) | (padded[2] << 8) | padded[3]) >>>
    0;
  const lo =
    ((padded[4] << 24) | (padded[5] << 16) | (padded[6] << 8) | padded[7]) >>>
    0;
  return hi * 0x100000000 + lo;
}

/**
 * 字节数组（大端） → int64（位模式同 uint64，只是按有符号解释）
 *   注意：与 Go 版一致，先按 uint64 解析位模式，再按二补码解释
 * @param {Uint8Array} b
 * @returns {bigint|number}
 */
function to_int64(b) {
  const u = to_uint64(b);
  if (_HAS_BIGINT) {
    // uint64 bit pattern → int64: 超过 INT64_MAX 时减去 2^64
    const max = BigInt("9223372036854775807");
    const two64 = BigInt("18446744073709551616");
    return u > max ? u - two64 : u;
  }
  // Number: 当高位 >= 0x80000000 时为负数
  if (u >= 0x8000000000000000) {
    return u - 0x10000000000000000;
  }
  return u;
}

/* -------- 窄化辅助：对齐 Go 的 uint8/int8 等强制窄化语义 -------- */

/** 按 int8 窄化（wrap around）：int8(248) = -8 */
function narrowInt8(n) {
  const v = Number(n) & 0xff;
  return v >= 0x80 ? v - 0x100 : v;
}
/** 按 int16 窄化 */
function narrowInt16(n) {
  const v = Number(n) & 0xffff;
  return v >= 0x8000 ? v - 0x10000 : v;
}
/** 按 int32 窄化 */
function narrowInt32(n) {
  return Number(n) | 0;
}
/** 按 uint8 窄化 */
function narrowUint8(n) {
  return Number(n) & 0xff;
}
/** 按 uint16 窄化 */
function narrowUint16(n) {
  return Number(n) & 0xffff;
}
/** 按 uint32 窄化 */
function narrowUint32(n) {
  return Number(n) >>> 0;
}

/* ============================================================
 *  辅助函数：JSON Fallback
 * ============================================================ */

/**
 * Slice/Map/Struct 等复合类型用 JSON 转成字符串
 * 作为 AG String 帧 payload；调用方再用 JSON.parse(Data(frame)) 还原
 * @param {any} v
 * @returns {string}
 */
function jsonMarshalFallback(v) {
  return JSON.stringify(v);
}

/**
 * Custom 类型兜底序列化：优先 JSON，失败则 String()
 * @param {any} v
 * @returns {Uint8Array}
 */
function _serialize(v) {
  const enc = new TextEncoder();
  try {
    return enc.encode(JSON.stringify(v));
  } catch (_e) {
    return enc.encode(String(v));
  }
}

// ============================================================
// 内部辅助: 64位整数读写 (兼容有无 BigInt 的环境)
// 关键：此处 _HAS_BIGINT 已由顶部 __probeBigInt() 做过"真实可调用"检测
//       （不是 typeof BigInt==='function' 这种纸面判断，避免 installHook.js
//       mock 出的假 BigInt 把代码带进 try→catch→降级→Number(NaN)→抛错）。
// ============================================================

/* 兼容兜底 FnError：tools.js 比 fn.js 先加载（index_v3.html 顺序 1），
 * 自己不能引用 fn.js 的类，所以这里定义独立的 Error，保持名字一致即可，
 * 被 fn.js 的 instanceof 检测不到没关系，但 message 要可辨识。
 * 注意：因为 tools.js 源码内自己的 class FnError 是在 bundle 内 fn.js 合并
 * 后声明的，TDZ 下 typeof FnError 会 ReferenceError，这里只能先 try/catch
 * 访问 window/global 上已挂载的 FnError，绝对不能出现裸名 FnError 的 typeof */
(function __ensureToolsFnError(){
    try {
        var root = (typeof window !== 'undefined') ? window
                 : (typeof self   !== 'undefined') ? self
                 : (typeof global !== 'undefined') ? global
                 : (typeof globalThis !== 'undefined') ? globalThis
                 : Function('return this')();
        if (root && root.FnError) return;       // 宿主已定义就不覆盖
        if (root) {
            root.FnError = (function(){
                function FnError(msg, cause) {
                    var base = Error.call(this, msg);
                    this.name    = 'FnError';
                    this.message = msg;
                    if (cause) this.cause = cause;
                    if (Error.captureStackTrace) Error.captureStackTrace(this, FnError);
                    else this.stack = base.stack;
                }
                FnError.prototype = Object.create(Error.prototype);
                FnError.prototype.constructor = FnError;
                return FnError;
            })();
        }
    } catch (_e) { /* 静默 */ }
})();

/**
 * 以大端序写入 uint64 到 DataView（和 fn.js/sock_rpc_v3.js 行为一致）
 * @private
 */
function writeUint64BE(view, offset, value) {
  // --- 分支 1：真实 BigInt 支持（探针通过） ---
  if (_HAS_BIGINT) {
    try {
      var asBigInt = (typeof value === 'bigint')
          ? value
          : BigInt((typeof value === 'string' || typeof value === 'number') ? value : String(value));
      view.setBigUint64(offset, asBigInt, false);
      return;
    } catch (e) {
      // 降级
    }
  }
  // --- 分支 2：Number 降级（hi/lo 双 32-bit 拆分） ---
  var num = NaN;
  try {
    if (value == null) num = 0;
    else if (typeof value === 'bigint') num = Number(value) || 0;
    else num = Number(value);
  } catch (_e) { num = NaN; }

  if (!isFinite(num) || num < 0) {
    throw new FnError(
      "fn: invalid uint64 value (no BigInt support in this environment, and value cannot be converted to finite number)"
    );
  }
  var MAX_U32 = 0xffffffff;
  var lo = ((num % 0x100000000) | 0) >>> 0;
  var hi = ((num / 0x100000000) | 0) >>> 0;
  if (typeof value === 'string' && /^\d+$/.test(value)) {
    var s = value.replace(/^0+/, '') || '0';
    if (s.length <= 15) {
      var n2 = Number(s);
      lo = ((n2 % 0x100000000) | 0) >>> 0;
      hi = ((n2 / 0x100000000) | 0) >>> 0;
    } else {
      try {
        var bv = BigInt(s);
        lo = Number(bv & BigInt(0xffffffff)) >>> 0;
        hi = Number((bv >> BigInt(32)) & BigInt(0xffffffff)) >>> 0;
      } catch (_ignore) { /* 保持 Number 拆分 */ }
    }
  }
  view.setUint32(offset,      (hi & MAX_U32) >>> 0, false);
  view.setUint32(offset + 4,  (lo & MAX_U32) >>> 0, false);
}

/**
 * 以大端序从 DataView 读取 uint64（和 fn.js 行为一致）
 * @private
 */
function readUint64BE(view, offset) {
  if (_HAS_BIGINT) {
    try {
      return view.getBigUint64(offset, false);
    } catch (e) {
      // 降级
    }
  }
  var hi = view.getUint32(offset,     false) >>> 0;
  var lo = view.getUint32(offset + 4, false) >>> 0;
  if (hi <= 0x1fffff) return (hi * 0x100000000) + lo;
  try {
    if (typeof BigInt === 'function') {
      return (BigInt(hi) << BigInt(32)) | BigInt(lo);
    }
  } catch (_e) { /* fall through */ }
  if (typeof console !== 'undefined' && console.warn) {
    console.warn(
      "[fn.js] uint64 value exceeds Number.MAX_SAFE_INTEGER, precision may be lost. Enable real BigInt for accuracy."
    );
  }
  return (hi * 0x100000000) + lo;
}

// CRC functions
const crc16_h = new Uint8Array([
  0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41, 0x00,
  0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1,
  0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81,
  0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40,
  0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x01,
  0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0,
  0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80,
  0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41,
  0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00,
  0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0,
  0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80,
  0x41, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41,
  0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x01,
  0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1,
  0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81,
  0x40, 0x01, 0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40,
  0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1, 0x81, 0x40, 0x01,
  0xc0, 0x80, 0x41, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x00, 0xc1,
  0x81, 0x40, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40, 0x01, 0xc0, 0x80,
  0x41, 0x01, 0xc0, 0x80, 0x41, 0x00, 0xc1, 0x81, 0x40,
]);

const crc16_l = new Uint8Array([
  0x00, 0xc0, 0xc1, 0x01, 0xc3, 0x03, 0x02, 0xc2, 0xc6, 0x06, 0x07, 0xc7, 0x05,
  0xc5, 0xc4, 0x04, 0xcc, 0x0c, 0x0d, 0xcd, 0x0f, 0xcf, 0xce, 0x0e, 0x0a, 0xca,
  0xcb, 0x0b, 0xc9, 0x09, 0x08, 0xc8, 0xd8, 0x18, 0x19, 0xd9, 0x1b, 0xdb, 0xda,
  0x1a, 0x1e, 0xde, 0xdf, 0x1f, 0xdd, 0x1d, 0x1c, 0xdc, 0x14, 0xd4, 0xd5, 0x15,
  0xd7, 0x17, 0x16, 0xd6, 0xd2, 0x12, 0x13, 0xd3, 0x11, 0xd1, 0xd0, 0x10, 0xf0,
  0x30, 0x31, 0xf1, 0x33, 0xf3, 0xf2, 0x32, 0x36, 0xf6, 0xf7, 0x37, 0xf5, 0x35,
  0x34, 0xf4, 0x3c, 0xfc, 0xfd, 0x3d, 0xff, 0x3f, 0x3e, 0xfe, 0xfa, 0x3a, 0x3b,
  0xfb, 0x39, 0xf9, 0xf8, 0x38, 0x28, 0xe8, 0xe9, 0x29, 0xeb, 0x2b, 0x2a, 0xea,
  0xee, 0x2e, 0x2f, 0xef, 0x2d, 0xed, 0xec, 0x2c, 0xe4, 0x24, 0x25, 0xe5, 0x27,
  0xe7, 0xe6, 0x26, 0x22, 0xe2, 0xe3, 0x23, 0xe1, 0x21, 0x20, 0xe0, 0xa0, 0x60,
  0x61, 0xa1, 0x63, 0xa3, 0xa2, 0x62, 0x66, 0xa6, 0xa7, 0x67, 0xa5, 0x65, 0x64,
  0xa4, 0x6c, 0xac, 0xad, 0x6d, 0xaf, 0x6f, 0x6e, 0xae, 0xaa, 0x6a, 0x6b, 0xab,
  0x69, 0xa9, 0xa8, 0x68, 0x78, 0xb8, 0xb9, 0x79, 0xbb, 0x7b, 0x7a, 0xba, 0xbe,
  0x7e, 0x7f, 0xbf, 0x7d, 0xbd, 0xbc, 0x7c, 0xb4, 0x74, 0x75, 0xb5, 0x77, 0xb7,
  0xb6, 0x76, 0x72, 0xb2, 0xb3, 0x73, 0xb1, 0x71, 0x70, 0xb0, 0x50, 0x90, 0x91,
  0x51, 0x93, 0x53, 0x52, 0x92, 0x96, 0x56, 0x57, 0x97, 0x55, 0x95, 0x94, 0x54,
  0x9c, 0x5c, 0x5d, 0x9d, 0x5f, 0x9f, 0x9e, 0x5e, 0x5a, 0x9a, 0x9b, 0x5b, 0x99,
  0x59, 0x58, 0x98, 0x88, 0x48, 0x49, 0x89, 0x4b, 0x8b, 0x8a, 0x4a, 0x4e, 0x8e,
  0x8f, 0x4f, 0x8d, 0x4d, 0x4c, 0x8c, 0x44, 0x84, 0x85, 0x45, 0x87, 0x47, 0x46,
  0x86, 0x82, 0x42, 0x43, 0x83, 0x41, 0x81, 0x80, 0x40,
]);

// CRC计算（与 Go 端 internal/utils/crc.go 的 GetCrC 完全一致）
function getCRC(data) {
  let hi = 0x00ff;
  let low = 0x00ff;
  for (let i = 0; i < data.length; i++) {
    const pos = (low ^ data[i]) & 0x00ff;
    low = hi ^ crc16_h[pos];
    hi = crc16_l[pos];
  }
  const d_crc = ((hi & 0x00ff) << 8) | (low & 0x00ff);
  const d_crcArr = new Uint8Array(2);
  d_crcArr[0] = d_crc & 0xff;
  d_crcArr[1] = (d_crc >> 8) & 0xff;
  return d_crcArr;
}


/* ============================================================
 * source: slice.js
 * ============================================================ */
/* patch: slice.js 错误引用 GetCrC，自动别名 getCRC */
if (typeof GetCrC === "undefined" && typeof getCRC !== "undefined") { var GetCrC = getCRC; }
// Constants
const TextMessage = 0x01;
const BinaryMessage = 0x02;
const LongMessage = 0x80;
const CRC = 0x40;

// DataSlice class
class DataSlice {
    constructor(p, n, t, i, s, d) {
        this.P = p; // Message type
        this.N = n; // Slice name (2 bytes)
        this.T = t; // Total slices
        this.I = i; // Current slice index
        this.S = s; // Total message size
        this.D = d; // Slice data
    }

    Bytes() {
        return serialize(this);
    }

    MuskCheck() {
        return this.P & 0x40;
    }

    Encode(opts = []) {
        return Encode(this, opts);
    }
}



function newOption(opts = []) {
    const opt = new Option();
    for (const o of opts) {
        o(opt);
    }
    return opt;
}

function get_header_size(lLen, checkCRC) {
    let c = 0x02;
    if (!checkCRC) {
        c = 0;
    }
    return lLen + 1 + 2 + 1 + 1 + c;
}


function serialize(v) {
    try {
        return new TextEncoder().encode(JSON.stringify(v));
    } catch (err) {
        return new Uint8Array(0);
    }
}

// Frame options
class Option {
    constructor() {
        this.CheckCRC = false;
        this.LengthSize = 2;
    }
}

function CheckCRC() {
    return function(opt) {
        opt.CheckCRC = true;
    };
}

function IsComplete(src, dst) {
    if (!src || !dst) return false;
    if (src.length !== 2 || dst.length !== 2) return false;
    return src[0] === dst[0] && src[1] === dst[1];
}

function CheckCRC(src, crc) {
    return IsComplete(getCRC(src), crc);
}

// Encode function
function Encode(s, opts = []) {
    const opt = newOption(opts);
    // 1byte type
    // 2byte name
    // 1byte slices
    // 1byte index
    // 2/4byte size
    // 0/2byte crc
    // nbyte data
    let tag = s.P & 0x3F;
    let checkCRC = (s.P & CRC) === CRC;
    const l = s.D.length;
    
    // Determine if long message tag is needed based on length
    if (l <= 0xFFFF) {
        opt.LengthSize = 2;
    } else {
        tag |= LongMessage;
        opt.LengthSize = 4;
    }
    
    if (opt.CheckCRC || checkCRC) {
        checkCRC = true;
        tag |= CRC;
    }

    const headerSize = get_header_size(opt.LengthSize, checkCRC);
    const buf = new Uint8Array(headerSize + l);
    
    buf[0] = tag;
    
    // Write name (2 bytes)
    const nameBytes = new TextEncoder().encode(s.N);
    buf[1] = nameBytes[0] || 0;
    buf[2] = nameBytes[1] || 0;
    
    buf[3] = s.T;
    buf[4] = s.I;
    
    // Write length
    const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
    if (opt.LengthSize === 2) {
        dv.setUint16(5, l, false); // Big endian
    } else {
        dv.setUint32(5, l, false); // Big endian
    }
    
    // Write CRC if needed
    if (checkCRC) {
        const crc = getCRC(s.D);
        buf[headerSize - 2] = crc[0];
        buf[headerSize - 1] = crc[1];
    }
    
    // Write data
    buf.set(s.D, headerSize);
    
    return buf;
}

// Decode function
function Decode(b) {
    let headerSize = get_header_size(2, false);
    if (b.length < headerSize) {
        throw new Error("invalid slice data length");
    }
    
    const tag = b[0];
    const opt = new Option();
    
    if ((tag & LongMessage) === 0) {
        opt.LengthSize = 2;
    } else {
        opt.LengthSize = 4;
    }
    
    if ((tag & CRC) === CRC) {
        opt.CheckCRC = true;
    }
    
    const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
    let l = dv.getUint16(5, false); // Big endian
    if (opt.LengthSize === 4) {
        l = dv.getUint32(5, false); // Big endian
    }
    
    headerSize = get_header_size(opt.LengthSize, opt.CheckCRC);
    if (b.length < headerSize + l) {
        throw new Error("invalid slice data length");
    }
    
    const s = new DataSlice();
    s.P = tag;
    s.N = new TextDecoder().decode(b.subarray(1, 3));
    s.T = b[3];
    s.I = b[4];
    s.S = l;
    
    const data = b.subarray(headerSize, headerSize + l);
    
    // Verify CRC if needed
    if (opt.CheckCRC) {
        const crc = b.subarray(headerSize - 2, headerSize);
        if (!CheckCRC(data, crc)) {
            throw new Error("invalid slice crc");
        }
    }
    
    s.D = data;
    return s;
}

// Export everything
if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        TextMessage,
        BinaryMessage,
        LongMessage,
        CRC,
        DataSlice,
        Encode,
        Decode,
        GetCrC,
        IsComplete,
        CheckCRC,
        newOption
    };
}

/* ============================================================
 * source: ag.js
 * ============================================================ */
/**
 * @file AG 协议 (Argument Grid) 参数帧格式 - JavaScript 实现
 *
 * 帧格式：
 *   MAGIC  :p   2 byte   0x3A 0x70  (ASCII ":p")
 *   TYPE   t    1 byte   ArgumentType* 枚举
 *   LEN    l    2 byte   big endian，Value 字节数 (0~65535)
 *   VALUE  d    l byte   payload，长度 = l
 *
 * 总帧最小 5 字节，最大 5 + 65535 = 65540 字节。
 *
 * 说明：
 *   - 整数编码采用 Big Endian + 简易压缩（去除前导 0，负数保留 FF 符号位）
 *   - Float32/Float64/Complex64/Complex128 采用 Little Endian（与 Go 版一致）
 *   - 复合类型（Array/Object/非 Uint8Array）降级为 JSON 字符串以 String 帧传输
 *   - 64 位整数使用 BigInt 保证精度；若运行环境不支持 BigInt，将退化为 Number
 */

/* ============================================================
 *  常量定义
 * ============================================================ */

/** MAGIC 首字节 ':' */
const ArgumentMagic1 = 0x3A;
/** MAGIC 次字节 'p' */
const ArgumentMagic2 = 0x70;
/** 帧头大小 = Magic(2) + Type(1) + Length(2) = 5 */
const ArgumentHeaderSize = 2 + 1 + 2;
/** Value 段最大字节数上限（>此值时报 ErrAgDataTooLarge）。
 *  注意：LEN 字段是 16 位无符号，实际最大可表达 65535；当 data 长度恰好 65536 时
 *  不会触发错误但会使 LEN 溢位（与 Go 版行为完全一致）。
 */
const ArgumentMaxDataSize = 1 << 16;

/**
 * 基本类型枚举（与 Go 原语一一对应，0x01~0x1F 为基础标量；0x20~0x3F 为复合/扩展）
 * @enum {number}
 */
const ArgumentType = {
  Nil: 1,
  Bool: 2,

  Int: 3,
  Int8: 4,
  Int16: 5,
  Int32: 6,
  Int64: 7,

  Uint: 8,
  Uint8: 9,
  Uint16: 10,
  Uint32: 11,
  Uint64: 12,
  Uintptr: 13,

  Float32: 14,
  Float64: 15,

  Complex64: 16,
  Complex128: 17,

  String: 18,
  Bytes: 19,

  Slice: 20,
  Map: 21,
  Struct: 22,
  Custom: 23,
};

/** 兼容 Go 常量命名的别名 */
const ArgumentTypeNil = ArgumentType.Nil;
const ArgumentTypeBool = ArgumentType.Bool;
const ArgumentTypeInt = ArgumentType.Int;
const ArgumentTypeInt8 = ArgumentType.Int8;
const ArgumentTypeInt16 = ArgumentType.Int16;
const ArgumentTypeInt32 = ArgumentType.Int32;
const ArgumentTypeInt64 = ArgumentType.Int64;
const ArgumentTypeUint = ArgumentType.Uint;
const ArgumentTypeUint8 = ArgumentType.Uint8;
const ArgumentTypeUint16 = ArgumentType.Uint16;
const ArgumentTypeUint32 = ArgumentType.Uint32;
const ArgumentTypeUint64 = ArgumentType.Uint64;
const ArgumentTypeUintptr = ArgumentType.Uintptr;
const ArgumentTypeFloat32 = ArgumentType.Float32;
const ArgumentTypeFloat64 = ArgumentType.Float64;
const ArgumentTypeComplex64 = ArgumentType.Complex64;
const ArgumentTypeComplex128 = ArgumentType.Complex128;
const ArgumentTypeString = ArgumentType.String;
const ArgumentTypeBytes = ArgumentType.Bytes;
const ArgumentTypeSlice = ArgumentType.Slice;
const ArgumentTypeMap = ArgumentType.Map;
const ArgumentTypeStruct = ArgumentType.Struct;
const ArgumentTypeCustom = ArgumentType.Custom;

/* ============================================================
 *  错误对象
 * ============================================================ */

const ErrAgTooShort = new Error('ag: payload too short for header');
const ErrAgBadMagic = new Error('ag: bad magic header, expect :p');
const ErrAgLengthMismatch = new Error('ag: payload length mismatch');
const ErrAgDataTooLarge = new Error(`ag: data length exceeds ${ArgumentMaxDataSize}`);
const ErrAgUnknownType = new Error('ag: unknown type tag');
const ErrAgInvalidHeader = new Error('ag: invalid header');

/* ============================================================
 *  帧结构：基础操作
 * ============================================================ */

/**
 * 构造一帧 AG 字节流
 *   Layout: [Magic1 Magic2 Type Len(BE,2B) Data...]
 * @param {number} t 类型标签 (ArgumentType*)
 * @param {Uint8Array|null} data Value 段
 * @returns {Uint8Array}
 * @throws {ErrAgDataTooLarge} 数据超过 65535 字节
 */
function encode_ag(t, data) {
  const payload = data || new Uint8Array(0);
  if (payload.length > ArgumentMaxDataSize) {
    throw ErrAgDataTooLarge;
  }
  const out = new Uint8Array(ArgumentHeaderSize + payload.length);
  out[0] = ArgumentMagic1;
  out[1] = ArgumentMagic2;
  out[2] = t & 0xFF;
  // 写入 2 字节 BigEndian 长度
  out[3] = (payload.length >> 8) & 0xFF;
  out[4] = payload.length & 0xFF;
  if (payload.length > 0) {
    out.set(payload, ArgumentHeaderSize);
  }
  return out;
}

/**
 * O(1) 校验帧完整性（magic + length 匹配）
 * @param {Uint8Array|ArrayBuffer|Array<number>} b
 * @returns {boolean}
 */
function IsArgument(b) {
  const buf = _asU8(b);
  if (!buf || buf.length < ArgumentHeaderSize) return false;
  if (buf[0] !== ArgumentMagic1 || buf[1] !== ArgumentMagic2) return false;
  const length = (buf[3] << 8) | buf[4];
  return buf.length === ArgumentHeaderSize + length;
}

/**
 * 纯验证；全部通过返回 null，否则抛/返回 Error
 * @param {Uint8Array} b
 * @returns {Error|null}
 */
function Validate(b) {
  const buf = _asU8(b);
  if (!buf || buf.length < ArgumentHeaderSize) return ErrAgTooShort;
  if (buf[0] !== ArgumentMagic1 || buf[1] !== ArgumentMagic2) return ErrAgBadMagic;
  const length = (buf[3] << 8) | buf[4];
  if (length > ArgumentMaxDataSize) return ErrAgDataTooLarge;
  if (buf.length !== ArgumentHeaderSize + length) return ErrAgLengthMismatch;
  return null;
}

/**
 * 解析帧头并返回 (type, value_bytes)；失败抛错
 * @param {Uint8Array} b
 * @returns {{t: number, v: Uint8Array}}
 */
function ag_get_frame(b) {
  const err = Validate(b);
  if (err) throw err;
  const buf = _asU8(b);
  const t = buf[2];
  const v =  ag_get_data(buf);
  return { t, v };
}

/**
 * 取 Value 段拷贝（按类型执行零扩展对齐）
 *   - 对 int8/uint8  →  1B zeroExtend
 *   - 对 int16/uint16 → 2B zeroExtend
 *   - 对 int32/uint32 → 4B zeroExtend
 *   - 对 64 位整数族  → 8B zeroExtend
 *   - 其他类型       → 原样拷贝
 *   Value 段为 0 字节时返回 null（与 Go 版保持一致）
 * @param {Uint8Array} b
 * @returns {Uint8Array|null}
 */
function ag_get_data(b) {
  const buf = _asU8(b);
  const length = (buf[3] << 8) | buf[4];
  if (length === 0) return null;
  const raw = new Uint8Array(length);
  raw.set(buf.subarray(ArgumentHeaderSize, ArgumentHeaderSize + length), 0);
  const t = buf[2];
  switch (t) {
    case ArgumentTypeUint8:
    case ArgumentTypeInt8:
      return zeroExtendN(raw, 1);
    case ArgumentTypeUint16:
    case ArgumentTypeInt16:
      return zeroExtend2byte(raw);
    case ArgumentTypeUint32:
    case ArgumentTypeInt32:
      return zeroExtend4byte(raw);
    case ArgumentTypeUint64:
    case ArgumentTypeInt64:
    case ArgumentTypeInt:
    case ArgumentTypeUint:
    case ArgumentTypeUintptr:
      return zeroExtend8byte(raw);
  }
  return raw;
}

/**
 * Data 取 Value 段；非 AG 帧或不合法返回源切片（兼容旧调用方直接透传）
 * @param {Uint8Array} b
 * @returns {Uint8Array|null}
 */
function Data(b) {
  if (!IsArgument(b)) return b;
  return ag_get_data(b);
}

/** Value = Data 别名 */
function Value(b) { return Data(b); }

/**
 * Decoder: 非 AG 帧返回原字节；AG 帧返回 Value 段
 *   与 Decode 的区别：不解码具体值，只取 payload
 * @param {Uint8Array} b
 * @returns {Uint8Array|null}
 */
function Decoder(b) {
  if (!IsArgument(b)) return b;
  return ag_get_data(b);
}

/* ============================================================
 *  类型判断：typeof
 * ============================================================ */

/**
 * 判断 JS 值对应的 AG 类型标签
 *   - JS 的 Number 无法区分 int/float；规则：
 *       · Number.isInteger 且绝对值 < 2^53  → 按 Int/Int64 处理
 *       · 否则 → Float64
 *   - Boolean        → Bool
 *   - null/undefined → Nil
 *   - string         → String
 *   - Uint8Array     → Bytes
 *   - Array          → Slice（实际编码会降级 JSON → String）
 *   - plain Object   → Struct/Map（实际编码会降级 JSON → String）
 *   - BigInt         → Int64 / Uint64（根据符号）
 * @param {any} arg
 * @returns {number} ArgumentType*
 */
function typeofTag(arg) {
  if (arg === null || arg === undefined) return ArgumentTypeNil;
  if (typeof arg === 'boolean') return ArgumentTypeBool;
  if (typeof arg === 'string') return ArgumentTypeString;
  if (typeof arg === 'bigint') {
    return arg < 0 ? ArgumentTypeInt64 : ArgumentTypeUint64;
  }
  if (typeof arg === 'number') {
    if (!Number.isFinite(arg)) return ArgumentTypeFloat64;
    if (Number.isInteger(arg)) {
      // JS 的 number 整数统一走 Int（64 位压缩）
      return ArgumentTypeInt;
    }
    return ArgumentTypeFloat64;
  }
  if (arg instanceof Uint8Array) return ArgumentTypeBytes;
  if (Array.isArray(arg)) return ArgumentTypeSlice;
  if (arg && typeof arg === 'object') {
    // 判断是否复数对象 {real,imag}
    if ('real' in arg && 'imag' in arg &&
        typeof arg.real === 'number' && typeof arg.imag === 'number') {
      // 默认 Complex128；调用方可通过显式包装指定 64
      return ArgumentTypeComplex128;
    }
    return ArgumentTypeStruct;
  }
  return ArgumentTypeCustom;
}

/* ============================================================
 *  编码：EncodeArg / Encoder / Json
 * ============================================================ */

/**
 * 把任意值按类型编码为一帧 AG
 *   - 标量走原语编码
 *   - 复合 (Array/Object/非 Uint8Array) 走 JSON fallback 映射成 String 帧
 * @param {any} arg
 * @returns {Uint8Array}
 * @throws {ErrAgDataTooLarge}
 */
function EncodeArg(arg) {
  if (arg === null || arg === undefined) {
    return encode_ag(ArgumentTypeNil, null);
  }
  const t = typeofTag(arg);
  switch (t) {
    case ArgumentTypeBool: {
      return encode_ag(t, new Uint8Array([arg ? 1 : 0]));
    }

    case ArgumentTypeInt:
    case ArgumentTypeInt8:
    case ArgumentTypeInt16:
    case ArgumentTypeInt32:
    case ArgumentTypeInt64: {
      return encode_ag(t, _intToByteImpl(_toBig(arg)));
    }

    case ArgumentTypeUint:
    case ArgumentTypeUint8:
    case ArgumentTypeUint16:
    case ArgumentTypeUint32:
    case ArgumentTypeUint64:
    case ArgumentTypeUintptr: {
      return encode_ag(t, uint_to_byte(arg));
    }

    case ArgumentTypeFloat32: {
      const buf = new ArrayBuffer(4);
      new DataView(buf).setFloat32(0, Number(arg), true); // LittleEndian
      return encode_ag(t, new Uint8Array(buf));
    }
    case ArgumentTypeFloat64: {
      const buf = new ArrayBuffer(8);
      new DataView(buf).setFloat64(0, Number(arg), true);
      return encode_ag(t, new Uint8Array(buf));
    }

    case ArgumentTypeComplex64: {
      const buf = new ArrayBuffer(8);
      const dv = new DataView(buf);
      dv.setFloat32(0, Number(arg.real), true);
      dv.setFloat32(4, Number(arg.imag), true);
      return encode_ag(t, new Uint8Array(buf));
    }
    case ArgumentTypeComplex128: {
      const buf = new ArrayBuffer(16);
      const dv = new DataView(buf);
      dv.setFloat64(0, Number(arg.real), true);
      dv.setFloat64(8, Number(arg.imag), true);
      return encode_ag(t, new Uint8Array(buf));
    }

    case ArgumentTypeString: {
      return encode_ag(t, new TextEncoder().encode(String(arg)));
    }
    case ArgumentTypeBytes: {
      const copy = new Uint8Array(arg.length);
      copy.set(arg, 0);
      return encode_ag(t, copy);
    }

    case ArgumentTypeSlice:
    case ArgumentTypeMap:
    case ArgumentTypeStruct: {
      const s = jsonMarshalFallback(arg);
      return encode_ag(ArgumentTypeString, new TextEncoder().encode(s));
    }
  }
  // 兜底：Custom 类型走 serialize
  return encode_ag(ArgumentTypeCustom, _serialize(arg));
}

/** Encoder = EncodeArg 别名 */
function Encoder(arg) { return EncodeArg(arg); }

/**
 * 优先用 Encode；若失败（理论上不会）则回退为 JSON String 帧
 *   这是 Go 版 `Json` 函数的等价实现（尽管 Go 版的 Encode 理论上不会错）
 * @param {any} v
 * @returns {Uint8Array}
 */
function Json(v) {
  try {
    return EncodeArg(v);
  } catch (_e) {
    const s = JSON.stringify(v);
    try {
      return encode_ag(ArgumentTypeString, new TextEncoder().encode(s));
    } catch (_e2) {
      return new Uint8Array(0);
    }
  }
}

/* ============================================================
 *  解码：Decode / get_value
 * ============================================================ */

/**
 * 根据 Type + Value 字节，还原出 JS 值
 * @param {number} t
 * @param {Uint8Array} v
 * @returns {any}
 */
function get_value_from(t, v) {
  switch (t) {
    case ArgumentTypeNil:
      return null;

    case ArgumentTypeBool:
      if (!v || v.length === 0) return false;
      return v[0] !== 0;

    case ArgumentTypeInt:
    case ArgumentTypeInt8:
    case ArgumentTypeInt16:
    case ArgumentTypeInt32:
    case ArgumentTypeInt64: {
      switch (t) {
        case ArgumentTypeInt8:   { const u = to_uint64(v); return narrowInt8(u); }
        case ArgumentTypeInt16:  { const u = to_uint64(v); return narrowInt16(u); }
        case ArgumentTypeInt32:  { const u = to_uint64(v); return narrowInt32(u); }
        case ArgumentTypeInt:    return Number(to_int64(v));   // Int 返回 Number（JS 常规整数）
        case ArgumentTypeInt64:  return to_int64(v);           // 保留完整 64 位（BigInt）
      }
      return Number(to_int64(v));
    }

    case ArgumentTypeUint:
    case ArgumentTypeUint8:
    case ArgumentTypeUint16:
    case ArgumentTypeUint32:
    case ArgumentTypeUint64:
    case ArgumentTypeUintptr: {
      const u = to_uint64(v);
      switch (t) {
        case ArgumentTypeUint8:   return narrowUint8(u);
        case ArgumentTypeUint16:  return narrowUint16(u);
        case ArgumentTypeUint32:  return narrowUint32(u);
        case ArgumentTypeUint:    return Number(u);   // Uint 返回 Number
        case ArgumentTypeUintptr: return Number(u);   // Uintptr 返回 Number
        case ArgumentTypeUint64:  return u;           // 完整 64 位（BigInt）
      }
      return Number(u);
    }

    case ArgumentTypeFloat32: {
      if (!v || v.length !== 4) throw ErrAgLengthMismatch;
      return new DataView(v.buffer, v.byteOffset, v.byteLength).getFloat32(0, true);
    }
    case ArgumentTypeFloat64: {
      if (!v || v.length !== 8) throw ErrAgLengthMismatch;
      return new DataView(v.buffer, v.byteOffset, v.byteLength).getFloat64(0, true);
    }

    case ArgumentTypeComplex64: {
      if (!v || v.length !== 8) throw ErrAgLengthMismatch;
      const dv = new DataView(v.buffer, v.byteOffset, v.byteLength);
      return { real: dv.getFloat32(0, true), imag: dv.getFloat32(4, true) };
    }
    case ArgumentTypeComplex128: {
      if (!v || v.length !== 16) throw ErrAgLengthMismatch;
      const dv = new DataView(v.buffer, v.byteOffset, v.byteLength);
      return { real: dv.getFloat64(0, true), imag: dv.getFloat64(8, true) };
    }

    case ArgumentTypeString:
      if (!v) return '';
      return new TextDecoder().decode(v);

    case ArgumentTypeBytes: {
      if (!v) return new Uint8Array(0);
      const out = new Uint8Array(v.length);
      out.set(v, 0);
      return out;
    }
  }
  throw ErrAgUnknownType;
}

/**
 * 从完整帧中取出 {type,value} 后再还原 JS 值
 * @param {Uint8Array} b
 * @returns {any}
 */
function get_value(b) {
  const { t, v } = ag_get_frame(b);
  return get_value_from(t, v);
}

/**
 * 解码一帧 AG → JS 值
 * @param {Uint8Array} b
 * @returns {any}
 * @throws {ErrAgInvalidHeader} 非合法 AG 帧
 */
function DecodeArg(b) {
  if (!IsArgument(b)) {
    throw ErrAgInvalidHeader;
  }
  return get_value(b);
}

/* ============================================================
 *  内部工具：输入规范化 → Uint8Array
 * ============================================================ */

function _asU8(b) {
  if (!b) return null;
  if (b instanceof Uint8Array) return b;
  if (b instanceof ArrayBuffer) return new Uint8Array(b);
  if (Array.isArray(b)) return new Uint8Array(b);
  if (typeof Buffer !== 'undefined' && b instanceof Buffer) {
    return new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  }
  // TypedArray 其他子类（如 Uint16Array）→ 取其底层字节
  if (ArrayBuffer.isView(b)) {
    return new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
  }
  return null;
}

/* ============================================================
 *  调试辅助：类型标签可读名（对标 Go 版 typeName）
 * ============================================================ */

/**
 * 返回 type tag 的可读名称（便于日志）
 * @param {number} t
 * @returns {string}
 */
function typeName(t) {
  switch (t) {
    case ArgumentTypeNil:        return 'nil';
    case ArgumentTypeBool:       return 'bool';
    case ArgumentTypeInt:        return 'int';
    case ArgumentTypeInt8:       return 'int8';
    case ArgumentTypeInt16:      return 'int16';
    case ArgumentTypeInt32:      return 'int32';
    case ArgumentTypeInt64:      return 'int64';
    case ArgumentTypeUint:       return 'uint';
    case ArgumentTypeUint8:      return 'uint8';
    case ArgumentTypeUint16:     return 'uint16';
    case ArgumentTypeUint32:     return 'uint32';
    case ArgumentTypeUint64:     return 'uint64';
    case ArgumentTypeUintptr:    return 'uintptr';
    case ArgumentTypeFloat32:    return 'float32';
    case ArgumentTypeFloat64:    return 'float64';
    case ArgumentTypeComplex64:  return 'complex64';
    case ArgumentTypeComplex128: return 'complex128';
    case ArgumentTypeString:     return 'string';
    case ArgumentTypeBytes:      return 'bytes';
    case ArgumentTypeSlice:      return 'slice';
    case ArgumentTypeMap:        return 'map';
    case ArgumentTypeStruct:     return 'struct';
    case ArgumentTypeCustom:     return 'custom';
  }
  return `unknown(${t})`;
}

/* ============================================================
 *  显式类型包装器：给 JS 调用方精确控制 Type Tag
 *  （因为 JS 的 Number 无法区分 int8 vs int64 等）
 * ============================================================ */

/**
 * 用显式类型标签包装值，使 EncodeArg 按指定类型而非自动推断编码
 *
 * 示例：
 *   EncodeArg(AsInt8(12))      → ArgumentTypeInt8
 *   EncodeArg(AsFloat32(3.14)) → ArgumentTypeFloat32
 *   EncodeArg(AsComplex64(1,2))→ ArgumentTypeComplex64
 *
 * @param {number} tag ArgumentType*
 * @param {any} val
 * @returns {{__ag_tag: number, __ag_val: any}}
 */
function Tagged(tag, val) {
  return { __ag_tag: tag, __ag_val: val };
}

function AsInt8(v)    { return Tagged(ArgumentTypeInt8, v); }
function AsInt16(v)   { return Tagged(ArgumentTypeInt16, v); }
function AsInt32(v)   { return Tagged(ArgumentTypeInt32, v); }
function AsInt64(v)   { return Tagged(ArgumentTypeInt64, v); }
function AsUint(v)    { return Tagged(ArgumentTypeUint, v); }
function AsUint8(v)   { return Tagged(ArgumentTypeUint8, v); }
function AsUint16(v)  { return Tagged(ArgumentTypeUint16, v); }
function AsUint32(v)  { return Tagged(ArgumentTypeUint32, v); }
function AsUint64(v)  { return Tagged(ArgumentTypeUint64, v); }
function AsUintptr(v) { return Tagged(ArgumentTypeUintptr, v); }
function AsFloat32(v) { return Tagged(ArgumentTypeFloat32, v); }
function AsComplex64(re, im) { return Tagged(ArgumentTypeComplex64, { real: re, imag: im }); }

/* ---- 在 Encode / typeofTag 中识别 Tagged 对象 ---- */

// 覆盖 typeofTag 增加 Tagged 识别
(function _patchTypeofTagAndEncode() {
  const _origTypeof = typeofTag;
  // 保存原 typeofTag，下面覆盖全局引用
  // （JS 函数声明提升，这里直接重写引用）
})();

// 为了让 Tagged 对象被 Encode 正确识别，我们需要 hook typeofTag 和 Encode
// 这里通过重新赋值来实现

const _origTypeofTag = typeofTag;
const _patchedTypeofTag = function (arg) {
  if (arg && typeof arg === 'object' && '__ag_tag' in arg && '__ag_val' in arg) {
    return arg.__ag_tag | 0;
  }
  return _origTypeofTag(arg);
};

const _origEncode = EncodeArg;
const _patchedEncode = function (arg) {
  if (arg && typeof arg === 'object' && '__ag_tag' in arg && '__ag_val' in arg) {
    const tag = arg.__ag_tag | 0;
    const val = arg.__ag_val;
    // 复用原 EncodeArg 的分支逻辑，但用显式 tag
    if (val === null || val === undefined) {
      return encode_ag(tag, null);
    }
    switch (tag) {
      case ArgumentTypeBool:
        return encode_ag(tag, new Uint8Array([val ? 1 : 0]));
      case ArgumentTypeInt:
      case ArgumentTypeInt8:
      case ArgumentTypeInt16:
      case ArgumentTypeInt32:
      case ArgumentTypeInt64:
        return encode_ag(tag, _intToByteImpl(_toBig(val)));
      case ArgumentTypeUint:
      case ArgumentTypeUint8:
      case ArgumentTypeUint16:
      case ArgumentTypeUint32:
      case ArgumentTypeUint64:
      case ArgumentTypeUintptr:
        return encode_ag(tag, uint_to_byte(val));
      case ArgumentTypeFloat32: {
        const buf = new ArrayBuffer(4);
        new DataView(buf).setFloat32(0, Number(val), true);
        return encode_ag(tag, new Uint8Array(buf));
      }
      case ArgumentTypeFloat64: {
        const buf = new ArrayBuffer(8);
        new DataView(buf).setFloat64(0, Number(val), true);
        return encode_ag(tag, new Uint8Array(buf));
      }
      case ArgumentTypeComplex64: {
        const buf = new ArrayBuffer(8);
        const dv = new DataView(buf);
        dv.setFloat32(0, Number(val.real), true);
        dv.setFloat32(4, Number(val.imag), true);
        return encode_ag(tag, new Uint8Array(buf));
      }
      case ArgumentTypeComplex128: {
        const buf = new ArrayBuffer(16);
        const dv = new DataView(buf);
        dv.setFloat64(0, Number(val.real), true);
        dv.setFloat64(8, Number(val.imag), true);
        return encode_ag(tag, new Uint8Array(buf));
      }
      case ArgumentTypeString:
        return encode_ag(tag, new TextEncoder().encode(String(val)));
      case ArgumentTypeBytes:
        return encode_ag(tag, new Uint8Array(val));
      case ArgumentTypeSlice:
      case ArgumentTypeMap:
      case ArgumentTypeStruct: {
        const s = jsonMarshalFallback(val);
        return encode_ag(ArgumentTypeString, new TextEncoder().encode(s));
      }
      case ArgumentTypeNil:
        return encode_ag(ArgumentTypeNil, null);
      case ArgumentTypeCustom:
        return encode_ag(ArgumentTypeCustom, _serialize(val));
    }
    // 未知标签 → 走原值自动推断
    return _origEncode(val);
  }
  return _origEncode(arg);
};

/* ============================================================
 *  导出（兼容 ESM / CJS / 浏览器全局）
 * ============================================================ */

const AGExports = {
  // 常量
  ArgumentMagic1,
  ArgumentMagic2,
  ArgumentHeaderSize,
  ArgumentMaxDataSize,
  ArgumentType,
  ArgumentTypeNil,
  ArgumentTypeBool,
  ArgumentTypeInt,
  ArgumentTypeInt8,
  ArgumentTypeInt16,
  ArgumentTypeInt32,
  ArgumentTypeInt64,
  ArgumentTypeUint,
  ArgumentTypeUint8,
  ArgumentTypeUint16,
  ArgumentTypeUint32,
  ArgumentTypeUint64,
  ArgumentTypeUintptr,
  ArgumentTypeFloat32,
  ArgumentTypeFloat64,
  ArgumentTypeComplex64,
  ArgumentTypeComplex128,
  ArgumentTypeString,
  ArgumentTypeBytes,
  ArgumentTypeSlice,
  ArgumentTypeMap,
  ArgumentTypeStruct,
  ArgumentTypeCustom,

  // 错误
  ErrAgTooShort,
  ErrAgBadMagic,
  ErrAgLengthMismatch,
  ErrAgDataTooLarge,
  ErrAgUnknownType,
  ErrAgInvalidHeader,

  // 辅助
  zeroExtendN,
  zeroExtend2byte,
  zeroExtend4byte,
  zeroExtend8byte,
  int_to_byte,
  uint_to_byte,
  to_int64,
  to_uint64,
  jsonMarshalFallback,
  typeName,

  // 帧处理
  encode_ag,
  IsArgument,
  Validate,
  ag_get_frame,
  ag_get_data,
  Data,
  Value,
  Decoder,

  // 编码/解码
  typeofTag: _patchedTypeofTag,
  EncodeArg: _patchedEncode,
  Encoder: _patchedEncode,
  DecodeArg,
  Json,
  get_value,
  get_value_from,

  // 显式类型包装
  Tagged,
  AsInt8,
  AsInt16,
  AsInt32,
  AsInt64,
  AsUint,
  AsUint8,
  AsUint16,
  AsUint32,
  AsUint64,
  AsUintptr,
  AsFloat32,
  AsComplex64,
};

// CommonJS / Node.js
if (typeof module !== 'undefined' && module.exports) {
  module.exports = AGExports;
}
// ESM 导出代理
if (typeof exports !== 'undefined') {
  Object.assign(exports, AGExports);
}
// 【V3 build 补丁】：在 bundle 合并 + 严格模式 IIFE 下，浏览器端也需要拿到 AG 裸名。
// 原写法是 if (typeof window!=='undefined') window.AG=...；但 Node require() 验证
// 用的是 global/globalThis，且没有 window。这里统一用 root 兜底，两边都 OK。
(function __exposeAG() {
  try {
    var root = (typeof globalThis !== 'undefined') ? globalThis
             : (typeof window     !== 'undefined') ? window
             : (typeof self       !== 'undefined') ? self
             : (typeof global     !== 'undefined') ? global
             : Function('return this')();
    if (root && typeof root.AG === 'undefined') root.AG = AGExports;
    // 把内部常用的辅助函数也顺便提一下（避免 terser 认为是内部变量 mangling 改名）
    var leak = ['EncodeArg','EncodeArg','Type','zeroExtendN','zeroExtend2byte','zeroExtend4byte','zeroExtend8byte'];
    for (var i = 0; i < leak.length; i++) {
      var k = leak[i];
      if (typeof root[k] === 'undefined' && typeof AGExports[k] !== 'undefined') root[k] = AGExports[k];
    }
  } catch (e) { /* 静默 */ }
})();
// 浏览器全局（保留原写法兼容其他宿主）
if (typeof window !== 'undefined') {
  if (typeof window.AG === 'undefined') window.AG = AGExports;
}
// Web Worker 全局
if (typeof self !== 'undefined' && typeof window === 'undefined') {
  if (typeof self.AG === 'undefined') self.AG = AGExports;
}


/* ============================================================
 * source: fn.js
 * ============================================================ */
/**
 * ============================================================
 * FN 协议帧编解码器 (JavaScript 版本)
 * 对应 Go 实现: fn.go
 * ============================================================
 *
 * 【协议帧结构】
 *
 *  偏移量   长度    字段名      类型             说明
 *  ------  ------  ----------  ---------------  ---------------------------
 *   0       2      Magic       uint8[2]         魔术字 = 0x40 0x46 ("@F")
 *   2       1      Action      uint8            动作类型 (不可为0)
 *   3       8      ID          uint64 BE        消息ID (大端序)
 *  11       4      Length      uint32 BE        Data 字段的字节长度 (大端序)
 *  15       N      Data        uint8[N]         数据载荷，长度 = Length
 *
 *  总头部长度 (HeaderSize) = 2 + 1 + 8 + 4 = 15 字节
 *  最大数据长度 (MaxDataSize) = 1 << 30 = 1,073,741,824 字节 (~1GB)
 *
 * ============================================================
 */

// ============================================================
// 常量定义
// ============================================================

/** 魔术字第1字节 */
const FnMagic1 = 0x40;

/** 魔术字第2字节 */
const FnMagic2 = 0x46;

/** 帧头部固定长度 = 15 字节 */
const FnHeaderSize = 2 + 1 + 8 + 4;

/** 数据载荷最大长度 = 1 << 30 (~1GB) */
const FnMaxDataSize = 1 << 30;

// ============================================================
// 兜底：writeUint64BE / readUint64BE（优先复用 tools.js 的全局实现）
// 说明：tools.js 在浏览器里声明了这两个全局函数；若因加载顺序问题
// 导致未定义，这里提供等价实现，避免 fn.js 单独使用时报 ReferenceError。
// ============================================================
(function _ensureUint64Helpers() {
    const _HAS_BIGINT = typeof BigInt === "function";
    if (typeof writeUint64BE !== 'function') {
        /**
         * 以大端序写入 uint64 到 DataView（兼容有无 BigInt）
         * @param {DataView} view
         * @param {number} offset
         * @param {bigint|number} value
         * @global
         */
        writeUint64BE = function (view, offset, value) {
            if (_HAS_BIGINT) {
                try {
                    view.setBigUint64(offset, typeof value === 'bigint' ? value : BigInt(value), false);
                    return;
                } catch (_e) { /* fallback */ }
            }
            const num = Number(value);
            if (!isFinite(num) || num < 0) {
                throw new FnError("fn: invalid uint64 value (no BigInt support in this environment)");
            }
            const MAX_U32 = 0xffffffff;
            const lo = (num & MAX_U32) >>> 0;
            const hi = (num / 0x100000000) | 0;
            view.setUint32(offset, (hi & MAX_U32) >>> 0, false);
            view.setUint32(offset + 4, lo, false);
        };
    }
    if (typeof readUint64BE !== 'function') {
        /**
         * 以大端序从 DataView 读取 uint64（兼容有无 BigInt）
         * @param {DataView} view
         * @param {number} offset
         * @returns {bigint|number}
         * @global
         */
        readUint64BE = function (view, offset) {
            if (_HAS_BIGINT) {
                try { return view.getBigUint64(offset, false); } catch (_e) { /* fallback */ }
            }
            const hi = view.getUint32(offset, false);
            const lo = view.getUint32(offset + 4, false);
            const combined = hi * 0x100000000 + lo;
            if (hi > 0x1fffff && typeof console !== 'undefined' && console.warn) {
                console.warn("[fn.js] uint64 exceeds Number.MAX_SAFE_INTEGER, precision may be lost. Enable BigInt for accuracy.");
            }
            return combined;
        };
    }
})();

// ============================================================
// 错误定义
// ============================================================

class FnError extends Error {
    constructor(message, cause) {
        super(message);
        this.name = 'FnError';
        if (cause) this.cause = cause;
    }
}

const ErrFnTooShort       = new FnError('fn: frame too short');
const ErrFnBadMagic       = new FnError('fn: bad magic header');
const ErrFnLengthMismatch = new FnError('fn: length field mismatch actual data');
const ErrFnDataTooLarge   = new FnError('fn: data size exceeds limit');
const ErrFnNilFrame       = new FnError('fn: nil frame');
const ErrFnInvalidFrame   = new FnError('fn: invalid frame');
const ErrFnInvalidAction  = new FnError('fn: invalid action (must be non-zero)');

// ============================================================
// 帧结构 (类定义)
// ============================================================

/**
 * FN 协议帧结构
 * @property {number} action - 动作类型 (uint8, 非零)
 * @property {bigint|number} id - 消息ID (uint64)
 * @property {Uint8Array} data - 数据载荷
 */
class FnFrame {
    /**
     * @param {number} action - 动作类型 (uint8)
     * @param {bigint|number} id - 消息ID (uint64)
     * @param {Uint8Array|null|undefined} data - 数据载荷
     */
    constructor(action, id, data) {
        this.action = action;
        this.id = id;
        this.data = data || new Uint8Array(0);
    }
}

// ============================================================
// 工具函数
// ============================================================

/**
 * 获取 FN 协议头部魔术字字节数组
 * @returns {Uint8Array} [0x40, 0x46]
 */
function FnHeader() {
    return new Uint8Array([FnMagic1, FnMagic2]);
}

/**
 * 判断字节数组是否是 FN 帧 (仅检查魔术字，不做完整校验)
 * @param {Uint8Array|ArrayBuffer|Array} b - 待检测数据
 * @returns {boolean} 是否以 FN 魔术字开头
 */
function IsFn(b) {
    if (!b) return false;
    const arr = toUint8Array(b);
    if (arr.length < FnHeaderSize || arr[0] !== FnMagic1 || arr[1] !== FnMagic2) {
        return false;
    }
    const view = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
    const length = view.getUint32(11, false);
    if (length > FnMaxDataSize) {
        return false;
    }
    const totalLen = FnHeaderSize + length;
    if (arr.length < totalLen) {
        return false;
    }
    return true;
}

/**
 * 把多种输入类型统一转换为 Uint8Array
 * @private
 * @param {Uint8Array|ArrayBuffer|Array<number>|null|undefined} input
 * @returns {Uint8Array}
 */
function toUint8Array(input) {
    if (input == null) return new Uint8Array(0);
    if (input instanceof Uint8Array) return input;
    if (input instanceof ArrayBuffer) return new Uint8Array(input);
    if (Array.isArray(input)) return new Uint8Array(input);
    // 尝试转换类数组对象
    if (typeof input.length === 'number' || input.byteLength != null) {
        return new Uint8Array(input);
    }
    throw new FnError('fn: unsupported data type for byte conversion');
}

// ============================================================
// 编码函数
// ============================================================

/**
 * 把 FnFrame 对象编码为字节数组
 * @param {FnFrame|null|undefined} f - 帧对象
 * @returns {{ buffer: Uint8Array, error: null } | { buffer: null, error: Error }} 编码结果
 */
function EncodeFn(f) {
    if (f == null) {
        return { buffer: null, error: ErrFnNilFrame };
    }
    const data = toUint8Array(f.data || new Uint8Array(0));
    return _encodeInternal(f.action, f.id, data);
}

/**
 * 编码 FN 帧 (便捷函数，直接传字段)
 * 如果 data 本身就是一个 FN 帧，则自动剥离其外层头部 (取内层 Data)
 * @param {number} action - 动作类型 (uint8)
 * @param {bigint|number} id - 消息ID (uint64)
 * @param {Uint8Array|ArrayBuffer|Array|null|undefined} data - 数据载荷
 * @returns {{ buffer: Uint8Array, error: null } | { buffer: null, error: Error }} 编码结果
 */
function Encode(action, id, data) {
    let payload = toUint8Array(data || new Uint8Array(0));
    if (IsFn(payload)) {
        payload = Data(payload);
    }
    return _encodeInternal(action, id, payload);
}

/**
 * 内部编码实现
 * @private
 */
function _encodeInternal(action, id, dataUint8) {
    const dataLen = dataUint8.length;
    if (dataLen > FnMaxDataSize) {
        return { buffer: null, error: ErrFnDataTooLarge };
    }

    const totalLen = FnHeaderSize + dataLen;
    const buf = new Uint8Array(totalLen);
    const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);

    // Magic
    buf[0] = FnMagic1;
    buf[1] = FnMagic2;
    // Action
    buf[2] = action & 0xFF;
    // ID (uint64 BigEndian)
    writeUint64BE(view, 3, id);
    // Length (uint32 BigEndian)
    view.setUint32(11, dataLen >>> 0, false);
    // Data
    if (dataLen > 0) {
        buf.set(dataUint8, FnHeaderSize);
    }

    return { buffer: buf, error: null };
}

// ============================================================
// 解码函数
// ============================================================

/**
 * 解码字节数组为 FnFrame 对象
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {{ frame: FnFrame, error: null } | { frame: null, error: Error }}
 */
function DecodeFn(b) {
    const arr = toUint8Array(b);
    const parsed = _decodeInternal(arr);
    if (parsed.error) {
        return { frame: null, error: parsed.error };
    }
    const { action, id, data } = parsed;
    return { frame: new FnFrame(action, id, data), error: null };
}

/**
 * 解码字节数组，直接返回各字段 (与 DecodeFn 逻辑相同，返回格式不同)
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {{ action: number, id: bigint|number, data: Uint8Array, error: null } | { action: 0, id: 0, data: null, error: Error }}
 */
function Decode(b) {
    const arr = toUint8Array(b);
    const parsed = _decodeInternal(arr);
    if (parsed.error) {
        return { action: 0, id: 0, data: null, error: parsed.error };
    }
    return { ...parsed, error: null };
}

/**
 * 内部解码实现
 * @private
 */
function _decodeInternal(arr) {
    if (arr.length < FnHeaderSize) {
        return { error: wrapError(ErrFnTooShort, `need ${FnHeaderSize}, got ${arr.length}`) };
    }
    if (arr[0] !== FnMagic1 || arr[1] !== FnMagic2) {
        return { error: wrapError(ErrFnBadMagic, `got 0x${arr[0].toString(16).padStart(2, '0').toUpperCase()}${arr[1].toString(16).padStart(2, '0').toUpperCase()}`) };
    }

    const view = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
    const length = view.getUint32(11, false); // BigEndian

    if (length > FnMaxDataSize) {
        return { error: ErrFnDataTooLarge };
    }

    const totalLen = FnHeaderSize + length;
    if (arr.length < totalLen) {
        return { error: wrapError(ErrFnLengthMismatch, `length=${length} total need ${totalLen}, got ${arr.length}`) };
    }

    const action = arr[2];
    const id = readUint64BE(view, 3);
    let data = new Uint8Array(0);
    if (length > 0) {
        data = arr.slice(FnHeaderSize, totalLen);
    }

    return { action, id, data, length };
}

// ============================================================
// 快速字段提取 (部分函数跳过完整校验，需调用方保证前置条件)
// ============================================================

/**
 * 快速提取帧 ID (建议先调用 Action 或 IsFn 校验帧有效性)
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {bigint|number} ID 值，数据过短时返回 0
 */
function Id(b) {
    const arr = toUint8Array(b);
    if (arr.length < 11) {
        return typeof BigInt !== 'undefined' ? 0n : 0;
    }
    const view = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
    return readUint64BE(view, 3);
}

/**
 * 提取帧 Action 字段 (会校验 Magic 和最小长度)
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {{ action: number, error: null } | { action: 0, error: Error }}
 */
function Action(b) {
    const arr = toUint8Array(b);
    if (arr.length < FnHeaderSize || arr[0] !== FnMagic1 || arr[1] !== FnMagic2) {
        return { action: 0, error: ErrFnInvalidFrame };
    }
    return { action: arr[2], error: null };
}

/**
 * 提取帧 Data 载荷
 * 如果输入本身不是 FN 帧，则原样返回；是 FN 帧则返回剥离头部后的 Data
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {Uint8Array}
 */
function Data(b) {
    const arr = toUint8Array(b);
    if (!IsFn(arr)) {
        return arr;
    }
    return arr.slice(FnHeaderSize);
}

// ============================================================
// 校验与头部解析
// ============================================================

/**
 * 完整校验帧合法性 (长度、Magic、Action 非零、长度匹配)
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {Error|null} 合法返回 null，否则返回错误
 */
function ValidateFn(b) {
    const arr = toUint8Array(b);

    if (arr.length < FnHeaderSize) {
        return wrapError(ErrFnTooShort, `need ${FnHeaderSize}, got ${arr.length}`);
    }
    if (arr[0] !== FnMagic1 || arr[1] !== FnMagic2) {
        return wrapError(ErrFnBadMagic, `got 0x${arr[0].toString(16).padStart(2, '0').toUpperCase()}${arr[1].toString(16).padStart(2, '0').toUpperCase()}`);
    }
    if (arr[2] === 0) {
        return ErrFnInvalidAction;
    }

    const view = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
    const length = view.getUint32(11, false);

    if (length > FnMaxDataSize) {
        return ErrFnDataTooLarge;
    }

    const totalLen = FnHeaderSize + length;
    if (arr.length < totalLen) {
        return wrapError(ErrFnLengthMismatch, `length=${length} total need ${totalLen}, got ${arr.length}`);
    }

    return null;
}

/**
 * 仅解析帧头部字段 (Action, ID, Length)，不处理 Data
 * @param {Uint8Array|ArrayBuffer|Array} b - 原始字节数据
 * @returns {{ action: number, id: bigint|number, length: number, error: null } | { action: 0, id: 0, length: 0, error: Error }}
 */
function ParseFnHeader(b) {
    const arr = toUint8Array(b);

    if (arr.length < FnHeaderSize) {
        return {
            action: 0, id: typeof BigInt !== 'undefined' ? 0n : 0, length: 0,
            error: wrapError(ErrFnTooShort, `need ${FnHeaderSize}, got ${arr.length}`)
        };
    }
    if (arr[0] !== FnMagic1 || arr[1] !== FnMagic2) {
        return {
            action: 0, id: typeof BigInt !== 'undefined' ? 0n : 0, length: 0,
            error: wrapError(ErrFnBadMagic, `got 0x${arr[0].toString(16).padStart(2, '0').toUpperCase()}${arr[1].toString(16).padStart(2, '0').toUpperCase()}`)
        };
    }

    const view = new DataView(arr.buffer, arr.byteOffset, arr.byteLength);
    const action = arr[2];
    const id = readUint64BE(view, 3);
    const length = view.getUint32(11, false);

    return { action, id, length, error: null };
}

/**
 * 包装错误信息，附带上下文
 * @private
 */
function wrapError(baseErr, detail) {
    const err = new FnError(`${baseErr.message}: ${detail}`);
    return err;
}

// ============================================================
// 导出 (兼容浏览器全局变量 / CommonJS / ES Module)
// ============================================================

const _exports = {
    // 常量
    FnMagic1,
    FnMagic2,
    FnHeaderSize,
    FnMaxDataSize,
    // 错误
    FnError,
    ErrFnTooShort,
    ErrFnBadMagic,
    ErrFnLengthMismatch,
    ErrFnDataTooLarge,
    ErrFnNilFrame,
    ErrFnInvalidFrame,
    ErrFnInvalidAction,
    // 类
    FnFrame,
    // 函数
    FnHeader,
    EncodeFn,
    Encode,
    DecodeFn,
    Decode,
    Id,
    Action,
    Data,
    ValidateFn,
    ParseFnHeader,
    IsFn,
};

// Node.js / CommonJS
if (typeof module !== 'undefined' && module.exports) {
    module.exports = _exports;
}

// ES Module
if (typeof __webpack_exports__ !== 'undefined' || (typeof window !== 'undefined' && window.__ES_MODULE__)) {
    // 兼容部分打包器
}

// 浏览器全局对象
if (typeof window !== 'undefined') {
    window.Fn = _exports;
}

// Web Worker
if (typeof self !== 'undefined' && typeof WorkerGlobalScope !== 'undefined' && self instanceof WorkerGlobalScope) {
    self.Fn = _exports;
}


/* ============================================================
 * source: sock_rpc_v3.js
 * ============================================================ */
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
 *        → slots.Client 包装层: 对每个 arg 执行 ag.EncodeArg(arg)
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
 *     · ACTION_REPLY_SUCCESS(0x02)：ID 匹配 Call 的 pending 回调，Data 为 AG 帧 → 自动 AG.DecodeArg 还原 JS 值
 *     · ACTION_REPLY_ERROR (0x03)：ID 匹配 pending 的 errCb，Data = UTF-8 错误描述
 *     · ACTION_CALL        (0x01)：服务器反向调用客户端 Bind 的本地服务
 *                                  Body = JsonCallObject JSON → Args[i] Base64→Uint8Array→AG.DecodeArg→JS 值
 *     · ACTION_BROADCAST  (0xFF)：无 ID，Data (自动 AG.DecodeArg) 广播给所有 OnMessage 监听器
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
     *       - 否则按 Bytes 类型先执行 AG.Encode(arg)（等价于 Go 的 `[]byte("sign") → ag.EncodeArg → Args[][]byte`）
     *   · 其他类型 → 直接 AG.Encode(arg)
     *
     * okCb 收到的值：服务器 Reply(data) 的 data 是 AG 帧时自动 AG.EncodeArg(data) 还原 JS 值；
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
                argsBytes.push(this._ag() ? this._ag().EncodeArg(null) : new Uint8Array([0x3A, 0x70, 0x01, 0x00, 0x00]));
                continue;
            }
            if (_isUint8Array(arg)) {
                // 已经是 AG 帧？直接放入（允许用户自己预先编码复用）
                if (this._ag() && typeof this._ag().IsArgument === 'function' && this._ag().IsArgument(arg)) {
                    argsBytes.push(new Uint8Array(arg));
                } else {
                    // 普通 Uint8Array → 作为 Bytes 类型 AG.Encode
                    argsBytes.push(this._ag().EncodeArg(arg));
                }
            } else {
                argsBytes.push(this._ag().EncodeArg(arg));
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
     *                      → Args[i] 每个 Base64→Uint8Array→AG.DecodeArg→JS 值
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
            const raw = args64[i];
            // Go 侧：JsonCallObject.Args 里每个元素都是 []byte 的 JSON/base64 表示；
            // 但有些参数是 nil / 0 值 / 纯字符串等，可能直接是 JSON 原值而不是 Base64。
            // 这里做“兼容解码”：
            //   1) null / undefined → null
            //   2) 字符串 → 先按 Base64 解码尝试 AG.DecodeArg；若不是合法 AG，则保留原字符串
            //   3) Uint8Array → 若是合法 AG 参数帧则 AG.DecodeArg，否则原样保留
            // 这样不会把 5 字节的短 payload（例如 nil/空 AG 帧）误当成 FN 帧去跑 Fn.DecodeArg。
            let u8 = raw;
            if (raw == null) {
                args.push(null);
                continue;
            }
            if (typeof raw === 'string') {
                const s = raw.trim();
                const maybe = _b64ToU8(s);
                if (AG && typeof AG.IsArgument === 'function' && AG.IsArgument(maybe)) {
                    u8 = maybe;
                } else if (s === '') {
                    args.push('');
                    continue;
                } else {
                    // 不是 Base64 AG 帧，保留原始 JSON 值（例如 "hello" / "123" / "true"）
                    args.push(raw);
                    continue;
                }
            }
            if (u8 instanceof Uint8Array) {
                const ag = this._ag();
                if (ag && _isFunction(ag.IsArgument) && ag.IsArgument(u8) && _isFunction(ag.DecodeArg)) {
                    try { args.push(ag.DecodeArg(u8)); continue; } catch (_) { /* fall through */ }
                }
            }
            args.push(u8);
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
            //  为什么不对返回值再 ag.EncodeArg？——
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
        if (ag && _isFunction(ag.IsArgument) && ag.IsArgument(u8) && _isFunction(ag.DecodeArg)) {
            try { return ag.DecodeArg(u8); } catch (_) { /* 解析失败就返回原字节 */ }
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


/* ============================================================
 * 共享 IIFE 尾部：把白名单显式挂到全局（兼容 浏览器 / Worker / 其他宿主）
 * ============================================================ */
  var __root__ = (typeof globalThis !== "undefined") ? globalThis
              : (typeof window     !== "undefined") ? window
              : (typeof self       !== "undefined") ? self
              : (typeof global     !== "undefined") ? global
              : Function("return this")();

  try { if (typeof _HAS_BIGINT !== "undefined") __root__["_HAS_BIGINT"] = _HAS_BIGINT; } catch (_e) {}
  try { if (typeof encode_bytes !== "undefined") __root__["encode_bytes"] = encode_bytes; } catch (_e) {}
  try { if (typeof decode_bytes !== "undefined") __root__["decode_bytes"] = decode_bytes; } catch (_e) {}
  try { if (typeof decode_string !== "undefined") __root__["decode_string"] = decode_string; } catch (_e) {}
  try { if (typeof _toBig !== "undefined") __root__["_toBig"] = _toBig; } catch (_e) {}
  try { if (typeof _bitAnd !== "undefined") __root__["_bitAnd"] = _bitAnd; } catch (_e) {}
  try { if (typeof _lshift !== "undefined") __root__["_lshift"] = _lshift; } catch (_e) {}
  try { if (typeof _urshift !== "undefined") __root__["_urshift"] = _urshift; } catch (_e) {}
  try { if (typeof zeroExtendN !== "undefined") __root__["zeroExtendN"] = zeroExtendN; } catch (_e) {}
  try { if (typeof zeroExtend2byte !== "undefined") __root__["zeroExtend2byte"] = zeroExtend2byte; } catch (_e) {}
  try { if (typeof zeroExtend4byte !== "undefined") __root__["zeroExtend4byte"] = zeroExtend4byte; } catch (_e) {}
  try { if (typeof zeroExtend8byte !== "undefined") __root__["zeroExtend8byte"] = zeroExtend8byte; } catch (_e) {}
  try { if (typeof int_to_byte !== "undefined") __root__["int_to_byte"] = int_to_byte; } catch (_e) {}
  try { if (typeof bi_holder !== "undefined") __root__["bi_holder"] = bi_holder; } catch (_e) {}
  try { if (typeof _intToByteImpl !== "undefined") __root__["_intToByteImpl"] = _intToByteImpl; } catch (_e) {}
  try { if (typeof uint_to_byte !== "undefined") __root__["uint_to_byte"] = uint_to_byte; } catch (_e) {}
  try { if (typeof to_uint64 !== "undefined") __root__["to_uint64"] = to_uint64; } catch (_e) {}
  try { if (typeof to_int64 !== "undefined") __root__["to_int64"] = to_int64; } catch (_e) {}
  try { if (typeof narrowInt8 !== "undefined") __root__["narrowInt8"] = narrowInt8; } catch (_e) {}
  try { if (typeof narrowInt16 !== "undefined") __root__["narrowInt16"] = narrowInt16; } catch (_e) {}
  try { if (typeof narrowInt32 !== "undefined") __root__["narrowInt32"] = narrowInt32; } catch (_e) {}
  try { if (typeof narrowUint8 !== "undefined") __root__["narrowUint8"] = narrowUint8; } catch (_e) {}
  try { if (typeof narrowUint16 !== "undefined") __root__["narrowUint16"] = narrowUint16; } catch (_e) {}
  try { if (typeof narrowUint32 !== "undefined") __root__["narrowUint32"] = narrowUint32; } catch (_e) {}
  try { if (typeof jsonMarshalFallback !== "undefined") __root__["jsonMarshalFallback"] = jsonMarshalFallback; } catch (_e) {}
  try { if (typeof _serialize !== "undefined") __root__["_serialize"] = _serialize; } catch (_e) {}
  try { if (typeof writeUint64BE !== "undefined") __root__["writeUint64BE"] = writeUint64BE; } catch (_e) {}
  try { if (typeof readUint64BE !== "undefined") __root__["readUint64BE"] = readUint64BE; } catch (_e) {}
  try { if (typeof crc16_h !== "undefined") __root__["crc16_h"] = crc16_h; } catch (_e) {}
  try { if (typeof crc16_l !== "undefined") __root__["crc16_l"] = crc16_l; } catch (_e) {}
  try { if (typeof getCRC !== "undefined") __root__["getCRC"] = getCRC; } catch (_e) {}
  try { if (typeof TextMessage !== "undefined") __root__["TextMessage"] = TextMessage; } catch (_e) {}
  try { if (typeof BinaryMessage !== "undefined") __root__["BinaryMessage"] = BinaryMessage; } catch (_e) {}
  try { if (typeof LongMessage !== "undefined") __root__["LongMessage"] = LongMessage; } catch (_e) {}
  try { if (typeof CRC !== "undefined") __root__["CRC"] = CRC; } catch (_e) {}
  try { if (typeof DataSlice !== "undefined") __root__["DataSlice"] = DataSlice; } catch (_e) {}
  try { if (typeof newOption !== "undefined") __root__["newOption"] = newOption; } catch (_e) {}
  try { if (typeof get_header_size !== "undefined") __root__["get_header_size"] = get_header_size; } catch (_e) {}
  try { if (typeof serialize !== "undefined") __root__["serialize"] = serialize; } catch (_e) {}
  try { if (typeof Option !== "undefined") __root__["Option"] = Option; } catch (_e) {}
  try { if (typeof IsComplete !== "undefined") __root__["IsComplete"] = IsComplete; } catch (_e) {}
  try { if (typeof CheckCRC !== "undefined") __root__["CheckCRC"] = CheckCRC; } catch (_e) {}
  try { if (typeof Encode !== "undefined") __root__["Encode"] = Encode; } catch (_e) {}
  try { if (typeof Decode !== "undefined") __root__["Decode"] = Decode; } catch (_e) {}
  try { if (typeof GetCrC !== "undefined") __root__["GetCrC"] = GetCrC; } catch (_e) {}
  try { if (typeof SliceMessage !== "undefined") __root__["SliceMessage"] = SliceMessage; } catch (_e) {}
  try { if (typeof Slice !== "undefined") __root__["Slice"] = Slice; } catch (_e) {}
  try { if (typeof newSliceText !== "undefined") __root__["newSliceText"] = newSliceText; } catch (_e) {}
  try { if (typeof newSliceBinary !== "undefined") __root__["newSliceBinary"] = newSliceBinary; } catch (_e) {}
  try { if (typeof decodeSliceText !== "undefined") __root__["decodeSliceText"] = decodeSliceText; } catch (_e) {}
  try { if (typeof decodeSliceBinary !== "undefined") __root__["decodeSliceBinary"] = decodeSliceBinary; } catch (_e) {}
  try { if (typeof concatSliceBinary !== "undefined") __root__["concatSliceBinary"] = concatSliceBinary; } catch (_e) {}
  try { if (typeof SliceTypes !== "undefined") __root__["SliceTypes"] = SliceTypes; } catch (_e) {}
  try { if (typeof SliceSize !== "undefined") __root__["SliceSize"] = SliceSize; } catch (_e) {}
  try { if (typeof FnMagic1 !== "undefined") __root__["FnMagic1"] = FnMagic1; } catch (_e) {}
  try { if (typeof FnMagic2 !== "undefined") __root__["FnMagic2"] = FnMagic2; } catch (_e) {}
  try { if (typeof FnHeaderSize !== "undefined") __root__["FnHeaderSize"] = FnHeaderSize; } catch (_e) {}
  try { if (typeof FnMaxDataSize !== "undefined") __root__["FnMaxDataSize"] = FnMaxDataSize; } catch (_e) {}
  try { if (typeof FnError !== "undefined") __root__["FnError"] = FnError; } catch (_e) {}
  try { if (typeof ErrFnTooShort !== "undefined") __root__["ErrFnTooShort"] = ErrFnTooShort; } catch (_e) {}
  try { if (typeof ErrFnBadMagic !== "undefined") __root__["ErrFnBadMagic"] = ErrFnBadMagic; } catch (_e) {}
  try { if (typeof ErrFnLengthMismatch !== "undefined") __root__["ErrFnLengthMismatch"] = ErrFnLengthMismatch; } catch (_e) {}
  try { if (typeof ErrFnDataTooLarge !== "undefined") __root__["ErrFnDataTooLarge"] = ErrFnDataTooLarge; } catch (_e) {}
  try { if (typeof ErrFnNilFrame !== "undefined") __root__["ErrFnNilFrame"] = ErrFnNilFrame; } catch (_e) {}
  try { if (typeof ErrFnInvalidFrame !== "undefined") __root__["ErrFnInvalidFrame"] = ErrFnInvalidFrame; } catch (_e) {}
  try { if (typeof ErrFnInvalidAction !== "undefined") __root__["ErrFnInvalidAction"] = ErrFnInvalidAction; } catch (_e) {}
  try { if (typeof FnFrame !== "undefined") __root__["FnFrame"] = FnFrame; } catch (_e) {}
  try { if (typeof FnHeader !== "undefined") __root__["FnHeader"] = FnHeader; } catch (_e) {}
  try { if (typeof IsFn !== "undefined") __root__["IsFn"] = IsFn; } catch (_e) {}
  try { if (typeof toUint8Array !== "undefined") __root__["toUint8Array"] = toUint8Array; } catch (_e) {}
  try { if (typeof EncodeFn !== "undefined") __root__["EncodeFn"] = EncodeFn; } catch (_e) {}
  try { if (typeof Encode !== "undefined") __root__["Encode"] = Encode; } catch (_e) {}
  try { if (typeof _encodeInternal !== "undefined") __root__["_encodeInternal"] = _encodeInternal; } catch (_e) {}
  try { if (typeof DecodeFn !== "undefined") __root__["DecodeFn"] = DecodeFn; } catch (_e) {}
  try { if (typeof Decode !== "undefined") __root__["Decode"] = Decode; } catch (_e) {}
  try { if (typeof _decodeInternal !== "undefined") __root__["_decodeInternal"] = _decodeInternal; } catch (_e) {}
  try { if (typeof Id !== "undefined") __root__["Id"] = Id; } catch (_e) {}
  try { if (typeof Action !== "undefined") __root__["Action"] = Action; } catch (_e) {}
  try { if (typeof Data !== "undefined") __root__["Data"] = Data; } catch (_e) {}
  try { if (typeof ValidateFn !== "undefined") __root__["ValidateFn"] = ValidateFn; } catch (_e) {}
  try { if (typeof ParseFnHeader !== "undefined") __root__["ParseFnHeader"] = ParseFnHeader; } catch (_e) {}
  try { if (typeof wrapError !== "undefined") __root__["wrapError"] = wrapError; } catch (_e) {}
  try { if (typeof _exports !== "undefined") __root__["_exports"] = _exports; } catch (_e) {}
  try { if (typeof ACTION_CALL !== "undefined") __root__["ACTION_CALL"] = ACTION_CALL; } catch (_e) {}
  try { if (typeof ACTION_REPLY_SUCCESS !== "undefined") __root__["ACTION_REPLY_SUCCESS"] = ACTION_REPLY_SUCCESS; } catch (_e) {}
  try { if (typeof ACTION_REPLY_ERROR !== "undefined") __root__["ACTION_REPLY_ERROR"] = ACTION_REPLY_ERROR; } catch (_e) {}
  try { if (typeof ACTION_BROADCAST !== "undefined") __root__["ACTION_BROADCAST"] = ACTION_BROADCAST; } catch (_e) {}
  try { if (typeof FN_HEADER_SIZE !== "undefined") __root__["FN_HEADER_SIZE"] = FN_HEADER_SIZE; } catch (_e) {}
  try { if (typeof FN_MAGIC_1 !== "undefined") __root__["FN_MAGIC_1"] = FN_MAGIC_1; } catch (_e) {}
  try { if (typeof FN_MAGIC_2 !== "undefined") __root__["FN_MAGIC_2"] = FN_MAGIC_2; } catch (_e) {}
  try { if (typeof FN !== "undefined") __root__["FN"] = FN; } catch (_e) {}
  try { if (typeof BuildCall !== "undefined") __root__["BuildCall"] = BuildCall; } catch (_e) {}
  try { if (typeof BuildReply !== "undefined") __root__["BuildReply"] = BuildReply; } catch (_e) {}
  try { if (typeof BuildBroadcast !== "undefined") __root__["BuildBroadcast"] = BuildBroadcast; } catch (_e) {}
  try { if (typeof ParseData !== "undefined") __root__["ParseData"] = ParseData; } catch (_e) {}
  try { if (typeof AGExports !== "undefined") __root__["AGExports"] = AGExports; } catch (_e) {}
  try { if (typeof AG !== "undefined") __root__["AG"] = AG; } catch (_e) {}
  try { if (typeof SockRpcV3 !== "undefined") __root__["SockRpcV3"] = SockRpcV3; } catch (_e) {}
  try { if (typeof TlvJson !== "undefined") __root__["TlvJson"] = TlvJson; } catch (_e) {}
  try { if (typeof TlvValue !== "undefined") __root__["TlvValue"] = TlvValue; } catch (_e) {}
  try { if (typeof TlvValueAsText !== "undefined") __root__["TlvValueAsText"] = TlvValueAsText; } catch (_e) {}
  try { if (typeof TlvValueAsJson !== "undefined") __root__["TlvValueAsJson"] = TlvValueAsJson; } catch (_e) {}

})();
