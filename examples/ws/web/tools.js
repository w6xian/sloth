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
