
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

// CRC16 查表（与 Go 端 internal/utils/crc.go 一致）
const crc16_h = new Uint8Array([
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
    0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
    0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1,
    0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1,
    0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40,
    0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1,
    0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40,
    0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
    0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
    0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
    0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40, 0x00, 0xC1, 0x81, 0x40,
    0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0, 0x80, 0x41, 0x00, 0xC1,
    0x81, 0x40, 0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41,
    0x00, 0xC1, 0x81, 0x40, 0x01, 0xC0, 0x80, 0x41, 0x01, 0xC0,
    0x80, 0x41, 0x00, 0xC1, 0x81, 0x40
]);
const crc16_l = new Uint8Array([
    0x00, 0xC0, 0xC1, 0x01, 0xC3, 0x03, 0x02, 0xC2, 0xC6, 0x06,
    0x07, 0xC7, 0x05, 0xC5, 0xC4, 0x04, 0xCC, 0x0C, 0x0D, 0xCD,
    0x0F, 0xCF, 0xCE, 0x0E, 0x0A, 0xCA, 0xCB, 0x0B, 0xC9, 0x09,
    0x08, 0xC8, 0xD8, 0x18, 0x19, 0xD9, 0x1B, 0xDB, 0xDA, 0x1A,
    0x1E, 0xDE, 0xDF, 0x1F, 0xDD, 0x1D, 0x1C, 0xDC, 0x14, 0xD4,
    0xD5, 0x15, 0xD7, 0x17, 0x16, 0xD6, 0xD2, 0x12, 0x13, 0xD3,
    0x11, 0xD1, 0xD0, 0x10, 0xF0, 0x30, 0x31, 0xF1, 0x33, 0xF3,
    0xF2, 0x32, 0x36, 0xF6, 0xF7, 0x37, 0xF5, 0x35, 0x34, 0xF4,
    0x3C, 0xFC, 0xFD, 0x3D, 0xFF, 0x3F, 0x3E, 0xFE, 0xFA, 0x3A,
    0x3B, 0xFB, 0x39, 0xF9, 0xF8, 0x38, 0x28, 0xE8, 0xE9, 0x29,
    0xEB, 0x2B, 0x2A, 0xEA, 0xEE, 0x2E, 0x2F, 0xEF, 0x2D, 0xED,
    0xEC, 0x2C, 0xE4, 0x24, 0x25, 0xE5, 0x27, 0xE7, 0xE6, 0x26,
    0x22, 0xE2, 0xE3, 0x23, 0xE1, 0x21, 0x20, 0xE0, 0xA0, 0x60,
    0x61, 0xA1, 0x63, 0xA3, 0xA2, 0x62, 0x66, 0xA6, 0xA7, 0x67,
    0xA5, 0x65, 0x64, 0xA4, 0x6C, 0xAC, 0xAD, 0x6D, 0xAF, 0x6F,
    0x6E, 0xAE, 0xAA, 0x6A, 0x6B, 0xAB, 0x69, 0xA9, 0xA8, 0x68,
    0x78, 0xB8, 0xB9, 0x79, 0xBB, 0x7B, 0x7A, 0xBA, 0xBE, 0x7E,
    0x7F, 0xBF, 0x7D, 0xBD, 0xBC, 0x7C, 0xB4, 0x74, 0x75, 0xB5,
    0x77, 0xB7, 0xB6, 0x76, 0x72, 0xB2, 0xB3, 0x73, 0xB1, 0x71,
    0x70, 0xB0, 0x50, 0x90, 0x91, 0x51, 0x93, 0x53, 0x52, 0x92,
    0x96, 0x56, 0x57, 0x97, 0x55, 0x95, 0x94, 0x54, 0x9C, 0x5C,
    0x5D, 0x9D, 0x5F, 0x9F, 0x9E, 0x5E, 0x5A, 0x9A, 0x9B, 0x5B,
    0x99, 0x59, 0x58, 0x98, 0x88, 0x48, 0x49, 0x89, 0x4B, 0x8B,
    0x8A, 0x4A, 0x4E, 0x8E, 0x8F, 0x4F, 0x8D, 0x4D, 0x4C, 0x8C,
    0x44, 0x84, 0x85, 0x45, 0x87, 0x47, 0x46, 0x86, 0x82, 0x42,
    0x43, 0x83, 0x41, 0x81, 0x80, 0x40
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

// CRC校验 (与 Go 端 CheckCRC 一致)
function checkCRC(data, crc) {
    if (!crc || crc.length !== 2) return false;
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