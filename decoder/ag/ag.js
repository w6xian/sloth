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
 *  工具函数：BigInt 支持检测
 * ============================================================ */

const _HAS_BIGINT = typeof BigInt === 'function';

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
function zeroExtend2byte(b) { return zeroExtendN(b, 2); }
/** 零扩展到 4 字节 */
function zeroExtend4byte(b) { return zeroExtendN(b, 4); }
/** 零扩展到 8 字节 */
function zeroExtend8byte(b) { return zeroExtendN(b, 8); }

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
  const mask = _toBig(0xFF);
  for (let idx = 7; idx >= 0; idx--) {
    b[idx] = Number(_bitAnd(bi, 0xFF));
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
function bi_holder(_v) { /* no-op, replaced by closure in real impl */ }

/**
 * int64 → 大端字节数组 + 压缩（真实实现）
 * @param {bigint|number} val
 * @returns {Uint8Array}
 */
function _intToByteImpl(val) {
  const b = new Uint8Array(8);
  let v = val;
  const ff = _toBig(0xFF);
  for (let idx = 7; idx >= 0; idx--) {
    b[idx] = Number(_bitAnd(v, 0xFF));
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
  if (_HAS_BIGINT && typeof dv.getBigUint64 === 'function') {
    return dv.getBigUint64(0, false); // big-endian 无符号
  }
  const hi = ((padded[0] << 24) | (padded[1] << 16) | (padded[2] << 8) | padded[3]) >>> 0;
  const lo = ((padded[4] << 24) | (padded[5] << 16) | (padded[6] << 8) | padded[7]) >>> 0;
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
    const max = BigInt('9223372036854775807');
    const two64 = BigInt('18446744073709551616');
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
  const v = Number(n) & 0xFF;
  return v >= 0x80 ? v - 0x100 : v;
}
/** 按 int16 窄化 */
function narrowInt16(n) {
  const v = Number(n) & 0xFFFF;
  return v >= 0x8000 ? v - 0x10000 : v;
}
/** 按 int32 窄化 */
function narrowInt32(n) {
  return Number(n) | 0;
}
/** 按 uint8 窄化 */
function narrowUint8(n) { return Number(n) & 0xFF; }
/** 按 uint16 窄化 */
function narrowUint16(n) { return Number(n) & 0xFFFF; }
/** 按 uint32 窄化 */
function narrowUint32(n) { return Number(n) >>> 0; }

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
function get_frame(b) {
  const err = Validate(b);
  if (err) throw err;
  const buf = _asU8(b);
  const t = buf[2];
  const v = get_data(buf);
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
function get_data(b) {
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
  return get_data(b);
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
  return get_data(b);
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
 *  编码：Encode / Encoder / Json
 * ============================================================ */

/**
 * 把任意值按类型编码为一帧 AG
 *   - 标量走原语编码
 *   - 复合 (Array/Object/非 Uint8Array) 走 JSON fallback 映射成 String 帧
 * @param {any} arg
 * @returns {Uint8Array}
 * @throws {ErrAgDataTooLarge}
 */
function Encode(arg) {
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

/** Encoder = Encode 别名 */
function Encoder(arg) { return Encode(arg); }

/**
 * 优先用 Encode；若失败（理论上不会）则回退为 JSON String 帧
 *   这是 Go 版 `Json` 函数的等价实现（尽管 Go 版的 Encode 理论上不会错）
 * @param {any} v
 * @returns {Uint8Array}
 */
function Json(v) {
  try {
    return Encode(v);
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
  const { t, v } = get_frame(b);
  return get_value_from(t, v);
}

/**
 * 解码一帧 AG → JS 值
 * @param {Uint8Array} b
 * @returns {any}
 * @throws {ErrAgInvalidHeader} 非合法 AG 帧
 */
function Decode(b) {
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
 * 用显式类型标签包装值，使 Encode 按指定类型而非自动推断编码
 *
 * 示例：
 *   Encode(AsInt8(12))      → ArgumentTypeInt8
 *   Encode(AsFloat32(3.14)) → ArgumentTypeFloat32
 *   Encode(AsComplex64(1,2))→ ArgumentTypeComplex64
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

const _origEncode = Encode;
const _patchedEncode = function (arg) {
  if (arg && typeof arg === 'object' && '__ag_tag' in arg && '__ag_val' in arg) {
    const tag = arg.__ag_tag | 0;
    const val = arg.__ag_val;
    // 复用原 Encode 的分支逻辑，但用显式 tag
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
  get_frame,
  get_data,
  Data,
  Value,
  Decoder,

  // 编码/解码
  typeofTag: _patchedTypeofTag,
  Encode: _patchedEncode,
  Encoder: _patchedEncode,
  Decode,
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
// 浏览器全局
if (typeof window !== 'undefined') {
  window.AG = AGExports;
}
// Web Worker 全局
if (typeof self !== 'undefined' && typeof window === 'undefined') {
  self.AG = AGExports;
}
