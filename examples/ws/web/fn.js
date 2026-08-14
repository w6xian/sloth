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
