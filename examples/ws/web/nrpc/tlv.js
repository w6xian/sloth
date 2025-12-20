
import Base64 from './base64';

// 错误定义
const ErrInvalidValueLength = new Error("value length is too long");
const ErrInvalidCrc = new Error("invalid crc");
const ErrInvalidFloat64 = new Error("invalid float64");
const ErrInvalidFloat64Type = new Error("invalid float64 type");
const ErrInvalidInt64 = new Error("invalid int64");
const ErrInvalidInt64Type = new Error("invalid int64 type");
const ErrInvalidUint64 = new Error("invalid uint64");
const ErrInvalidUint64Type = new Error("invalid uint64 type");
const ErrInvalidStructType = new Error("invalid type 0x00< tax >0x40(64)");
const ErrInvalidBinType = new Error("invalid binary type");
const ErrInvalidLengthSize = new Error("invalid length size,1-4");

// TLV类型常量
const TLV_TYPE_FRAME = 0x00;
const TLV_TYPE_STRING = 0x01;
const TLV_TYPE_JSON = 0x02;
const TLV_TYPE_BINARY = 0x03;
const TLV_TYPE_INT64 = 0x04;
const TLV_TYPE_UINT64 = 0x05;
const TLV_TYPE_FLOAT64 = 0x06;
const TLV_TYPE_BYTE = 0x07;
const TLV_TYPE_NIL = 0x08;

// 头部大小常量
const TLVX_HEADER_SIZE = 5;
const TLVX_HEADER_MIN_SIZE = 2;

// TlV类
class TlV {
    constructor(tag = 0, length = 0, value = new Uint8Array()) {
        this.T = tag;
        this.L = length;
        this.V = value;
    }

    Tag() { return this.T; }
    Type() { return this.T; }
    Value() { return this.V; }

    String() {
        return new TextDecoder().decode(this.V);
    }

    Json() {
        try {
            return JSON.parse(this.String());
        } catch (e) {
            throw new Error(`Failed to parse JSON: ${e.message}`);
        }
    }
}

// 从帧创建TLV
function NewTLVFromFrame(b, opts = []) {
    const t = new TlV();
    try {
        const [tag, data] = tlv_decode(b);
        t.T = tag;
        t.L = data.length;
        t.V = data;
        return t;
    } catch (err) {
        throw err;
    }
}

// 检查是否为有效的TLV帧
function IsTLVFrame(b) {
    try {
        tlv_decode(b);
        return true;
    } catch (err) {
        return false;
    }
}

// CRC计算 (需要根据Go的utils.GetCrC实现)
function getCRC(data) {
    const crc = new Uint8Array(2);
    // 实际CRC计算实现需要与Go版本匹配
    return crc;
}

// CRC校验 (需要根据Go的utils.CheckCRC实现)
function checkCRC(data, crc) {
    const calculatedCRC = getCRC(data);
    return calculatedCRC[0] === crc[0] && calculatedCRC[1] === crc[1];
}

// 计算头部大小
function get_header_size(lLen, checkCRC) {
    let c = 0x02;
    if (!checkCRC) c = 0;
    return lLen + 1 + c;
}

// TLV编码
function tlv_encode(tag, data, opts = []) {
    const opt = { CheckCRC: false, LengthSize: 1 };
    const l = data.length;

    if (l === 0x00) return new Uint8Array([tag, 0]);
    if (tag > 0x40) throw ErrInvalidStructType;
    if (l > 0xFFFF) throw ErrInvalidValueLength;

    // 确定长度大小
    if (l > 0xFF) {
        tag |= 0x80;
        opt.LengthSize = 2;
    }

    const headerSize = get_header_size(opt.LengthSize, opt.CheckCRC);
    const buf = new Uint8Array(headerSize + l);
    buf[0] = tag;

    if (opt.LengthSize === 2) buf[0] |= 0x80;
    if (opt.CheckCRC) buf[0] |= 0x40;

    // 写入长度
    const dv = new DataView(buf.buffer);
    if (opt.LengthSize === 1) {
        dv.setUint8(1, l);
    } else {
        dv.setUint16(1, l, false); // false表示大端序
    }

    // 写入CRC
    if (opt.CheckCRC) {
        const crc = getCRC(data);
        buf[headerSize - 2] = crc[0];
        buf[headerSize - 1] = crc[1];
    }

    // 写入数据
    buf.set(data, headerSize);
    return buf;
}

// TLV解码
function tlv_decode(b) {
    if (b.length < TLVX_HEADER_MIN_SIZE) throw ErrInvalidValueLength;

    let tag = b[0];
    let lengthSize = 1;
    let checkCRC = false;

    if ((tag & 0x80) > 0) lengthSize = 2;
    if ((tag & 0x40) > 0) checkCRC = true;
    tag &= 0x3F; // 提取低6位作为实际tag

    const headerSize = get_header_size(lengthSize, checkCRC);
    const dv = new DataView(b.buffer);
    let l = 0;

    switch (lengthSize) {
        case 1: l = dv.getUint8(1); break;
        case 2: l = dv.getUint16(1, false); break;
        default: throw ErrInvalidLengthSize;
    }

    if (b.length < headerSize + l) throw ErrInvalidValueLength;
    const dataBuf = b.subarray(headerSize, headerSize + l);

    if (checkCRC) {
        const crc = b.subarray(headerSize - 2, headerSize);
        if (!checkCRC(dataBuf, crc)) throw ErrInvalidCrc;
    }

    return [tag, dataBuf];
}

/**
    const str = 'hello world';
    const tlvFrame = frameFromString(str);
    console.log('编码后的 TLV 帧:', tlvFrame);

 */

/**
 * 将字符串转换为 TLV 帧
 * @param {string} v - 要编码的字符串
 * @returns {Uint8Array} 编码后的 TLV 帧
 */
function frameFromString(v) {
    try {
        // 将字符串转换为 UTF-8 字节数组
        const data = new TextEncoder().encode(v);
        // 调用 TLV 编码函数（之前实现的 tlv_encode）
        const frame = tlv_encode(TLV_TYPE_STRING, data);
        return frame;
    } catch (err) {
        // 错误处理：返回空数组
        console.error('TLV 编码失败:', err);
        return new Uint8Array();
    }
}


// 错误定义
const TLVErrors = {
    ErrInvalidValueLength: new Error('Invalid value length'),
    ErrInvalidFloat64: new Error('Invalid Float64 TLV frame'),
    ErrInvalidFloat64Type: new Error('Invalid Float64 type'),
    ErrInvalidInt64: new Error('Invalid Int64 TLV frame'),
    ErrInvalidInt64Type: new Error('Invalid Int64 type'),
    ErrInvalidUint64: new Error('Invalid Uint64 TLV frame'),
    ErrInvalidUint64Type: new Error('Invalid Uint64 type'),
    ErrInvalidStructType: new Error('Invalid Struct type'),
    ErrInvalidBinType: new Error('Invalid Binary type')
};



/**
 * JSON 对象转 TLV 帧
 * @param {any} v - 输入 JSON 对象
 * @returns {Uint8Array} TLV 帧
 */
function frameFromJson(v) {
    try {
        const jsonData = new TextEncoder().encode(JSON.stringify(v));
        const frame = tlv_encode(TLV_TYPE_JSON, jsonData);
        return frame;
    } catch (err) {
        return new Uint8Array();
    }
}

/**
 * 二进制数据转 TLV 帧
 * @param {Uint8Array} v - 二进制数据
 * @returns {Uint8Array} TLV 帧
 */
function frameFromBinary(v) {
    try {
        return tlv_encode(TLV_TYPE_BINARY, v);
    } catch (err) {
        return new Uint8Array();
    }
}

/**
 * Float64 转 TLV 帧
 * @param {number} v - 输入浮点数
 * @returns {Uint8Array} TLV 帧
 */
function frameFromFloat64(v) {
    try {
        const buffer = new ArrayBuffer(8);
        const view = new DataView(buffer);
        view.setFloat64(0, v, false); // 大端序
        const bytes = new Uint8Array(buffer);
        return tlv_encode(TLV_TYPE_FLOAT64, bytes);
    } catch (err) {
        return new Uint8Array();
    }
}

/**
 * Int64 转 TLV 帧
 * @param {number} v - 输入整数
 * @returns {Uint8Array} TLV 帧
 */
function frameFromInt64(v) {
    try {
        const buffer = new ArrayBuffer(8);
        const view = new DataView(buffer);
        view.setBigInt64(0, BigInt(v), false); // 大端序
        const bytes = new Uint8Array(buffer);
        return tlv_encode(TLV_TYPE_INT64, bytes);
    } catch (err) {
        return new Uint8Array();
    }
}

/**
 * Uint64 转 TLV 帧
 * @param {number} v - 输入无符号整数
 * @returns {Uint8Array} TLV 帧
 */
function frameFromUint64(v) {
    try {
        const buffer = new ArrayBuffer(8);
        const view = new DataView(buffer);
        view.setBigUint64(0, BigInt(v), false); // 大端序
        const bytes = new Uint8Array(buffer);
        return tlv_encode(TLV_TYPE_UINT64, bytes);
    } catch (err) {
        return new Uint8Array();
    }
}

/**
 * 字节数组转 Float64
 * @param {Uint8Array} v - 字节数组
 * @returns {number} 浮点数
 */
function bytes2Float64(v) {
    const view = new DataView(v.buffer);
    return view.getFloat64(0, false); // 大端序
}

/**
 * TLV 帧转 Float64
 * @param {Uint8Array} v - TLV 帧
 * @returns {number} 浮点数
 * @throws {Error} 转换错误
 */
function frameToFloat64(v) {
    if (v.length !== 8 + TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidFloat64;
    if (v[0] !== TLV_TYPE_FLOAT64) throw TLVErrors.ErrInvalidFloat64Type;
    return bytes2Float64(v.subarray(TLVX_HEADDER_SIZE));
}

/**
 * 字节数组转 Int64
 * @param {Uint8Array} v - 字节数组
 * @returns {bigint} 整数
 */
function bytes2Int64(v) {
    const view = new DataView(v.buffer);
    return view.getBigInt64(0, false); // 大端序
}

/**
 * TLV 帧转 Int64
 * @param {Uint8Array} v - TLV 帧
 * @returns {bigint} 整数
 * @throws {Error} 转换错误
 */
function frameToInt64(v) {
    if (v.length !== 8 + TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidInt64;
    if (v[0] !== TLV_TYPE_INT64) throw TLVErrors.ErrInvalidInt64Type;
    return bytes2Int64(v.subarray(TLVX_HEADDER_SIZE));
}

/**
 * 字节数组转 Uint64
 * @param {Uint8Array} v - 字节数组
 * @returns {bigint} 无符号整数
 */
function bytes2Uint64(v) {
    const view = new DataView(v.buffer);
    return view.getBigUint64(0, false); // 大端序
}

/**
 * TLV 帧转 Uint64
 * @param {Uint8Array} v - TLV 帧
 * @returns {bigint} 无符号整数
 * @throws {Error} 转换错误
 */
function frameToUint64(v) {
    if (v.length !== 8 + TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidUint64;
    if (v[0] !== TLV_TYPE_UINT64) throw TLVErrors.ErrInvalidUint64Type;
    return bytes2Uint64(v.subarray(TLVX_HEADDER_SIZE));
}

/**
 * TLV 帧转 JSON 对象
 * @param {Uint8Array} v - TLV 帧
 * @param {any} t - 目标对象
 * @returns {any} 解析后的对象
 * @throws {Error} 转换错误
 */
function frameToStruct(v, t) {
    if (!v || v.length < TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidValueLength;
    if (v[0] !== TLV_TYPE_JSON) throw TLVErrors.ErrInvalidStructType;
    const [, data] = tlv_decode(v);
    return JSON.parse(new TextDecoder().decode(data));
}

/**
 * TLV 帧转 JSON 对象
 * @param {Uint8Array} v - TLV 帧
 * @param {any} t - 目标对象
 * @returns {any} 解析后的对象
 * @throws {Error} 转换错误
 */
function frameToString(v, t) {
    if (!v || v.length < TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidValueLength;
    if (v[0] !== TLV_TYPE_STRING) throw TLVErrors.ErrInvalidStructType;
    const [, data] = tlv_decode(v);
    return String(new TextDecoder().decode(data));
}

/**
 * TLV 帧转二进制数据
 * @param {Uint8Array} v - TLV 帧
 * @returns {Uint8Array} 二进制数据
 * @throws {Error} 转换错误
 */
function frameToBin(v) {
    if (!v || v.length < TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidValueLength;
    if (v[0] !== TLV_TYPE_BINARY) throw TLVErrors.ErrInvalidBinType;
    const [, data] = tlv_decode(v);
    return data;
}

/**
 * 反序列化 TLV 帧
 * @param {Uint8Array} v - TLV 帧
 * @returns {Object} TLV 对象
 * @throws {Error} 转换错误
 */
function deserialize(v) {
    if (!v || v.length < TLVX_HEADDER_SIZE) throw TLVErrors.ErrInvalidValueLength;
    return NewTLVFromFrame(v);
}

/**
 * 序列化任意类型数据为 TLV 帧
 * @param {any} v - 任意类型数据
 * @returns {Uint8Array} TLV 帧
 */
function serialize(v) {
    if (v === null || v === undefined) return new Uint8Array();

    switch (typeof v) {
        case 'string':
            return frameFromString(v);
        case 'number':
            if (Number.isInteger(v)) {
                if (v >= 0) return frameFromUint64(BigInt(v));
                return frameFromInt64(BigInt(v));
            }
            return frameFromFloat64(v);
        case 'boolean':
            return frameFromInt64(BigInt(v ? 1 : 0));
        case 'object':
            if (v instanceof Uint8Array) return frameFromBinary(v);
            if (Array.isArray(v)) return frameFromJson(v);
            return frameFromJson(v);
        default:
            return frameFromJson(v);
    }
}

function parseTlvMessage(evt) {
    try {
        const data = Base64.decode(evt)
        const frame = new TextEncoder().encode(data);
        return NewTLVFromFrame(frame)
    } catch (error) {
        console.log(error)
    }
}

// Export everything
export {
    NewTLVFromFrame,
    // 帧转任意类型数据
    IsTLVFrame,
    frameToUint64,
    frameFromInt64,
    frameToInt64,
    frameToFloat64,
    frameToBin,
    frameToStruct,
    frameToString,
    // 转成tlv帧
    frameFromBinary,
    frameFromJson,
    frameFromFloat64,
    frameFromUint64,
    frameFromString,
    // 任意类型数据转成tlv帧
    deserialize,
    serialize,
    parseTlvMessage
}