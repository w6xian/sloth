var Code = {
    OPEN: 'onopen',
    READY: 'onopen',
    CLOSE: 'onclose',
    MESSAGE: 'onmessage',
    DATA: 'onmessage',
    ERROR: 'onerror',
}

function rpc_call_struct(method, data) {
    let callData = data;
    if (isObject(data)) {
        callData = JSON.stringify(data);
    } else if (isUint8Array(data)) {
        callData = data;
    }
    const callObj = {
        id: Date.now(),
        action: -0xFF, // -0xFE 调用服务
        method: method,
        data: callData,
    };
    return callObj;
}

class SockRpc {
    constructor(option) {
        if (isString(option)) {
            option = {
                addr: option,
            }
        }
        var options = option || {};
        this.addr = options.addr || '';
        this.sock = null;
        this.binaryType = options.binaryType || "blob";
        this.connected = false;
        this.listeners = {
            onopen: [],
            onclose: [],
            onmessage: [],
            onerror: [],
        };
        if (options.onOpen) {
            this.AddEvent(Code.OPEN, options.onOpen);
        }
        if (options.onClose) {
            this.AddEvent(Code.CLOSE, options.onClose);
        }
        if (options.onMessage) {
            this.AddEvent(Code.MESSAGE, options.onMessage);
        }
        if (options.onError) {
            this.AddEvent(Code.ERROR, options.onError);
        }
        this.rpcObj = {};
    }

    OnOpen(listener, options) {
        this.AddEvent(Code.OPEN, listener, options);
    }
    OnClose(listener, options) {
        this.AddEvent(Code.CLOSE, listener, options);
    }
    OnMessage(listener, options) {
        this.AddEvent(Code.MESSAGE, listener, options);
    }
    OnError(listener, options) {
        this.AddEvent(Code.ERROR, listener, options);
    }
    Bind(svr, obj) {
        if (this.rpcObj[svr]) {
            Object.assign(this.rpcObj[svr], obj || {});
        } else {
            this.rpcObj[svr] = obj || {};
        }
    }
    AddEvent(type, listener, options) {
        //建议 对应的Onxxxx 方法
        options = options || {};
        let ls = this.listeners[type];
        if (ls) {
            ls = ls.filter(res => res !== listener);
            // 去重
            ls.push(listener);
            // 有没有同Key的
            this.listeners[type] = ls;
        }
    }
    RemoveEvent(type, listener, options) {
        // eslint-disable-next-line no-param-reassign, @typescript-eslint/no-unused-vars
        options = options || {};
        let ls = this.listeners[type];
        if (ls) {
            // 去重
            ls = ls.filter(res => res !== listener);
            this.listeners[type] = ls;
        }
    }
    Stop() {
        if (this.sock) {
            this.sock.close();
            this.sock = null;
        }
        // 清空this.listeners
        this.listeners = {
            onopen: [],
            onclose: [],
            onmessage: [],
            onerror: [],
        };
    }
    Send(data) {
        if (isObject(data)) {
            data = JSON.stringify(data);
        }
        try {
            if (this.sock == null) {
                this.Connect({
                    ready: sock => {
                        send_message(sock, data);
                    },
                });
            } else {
                send_message(this.sock, data);
            }
        } catch (error) {
            console.log(error);
        }
    }
    /**
     * type JsonCallObject struct {
        Id     string `json:"id"`     // user id
        Action int    `json:"action"` // operation for request
        Method string `json:"method"` // service method name
        Data   string `json:"data"`   // binary body bytes
    }
     *
     */
    Call(method, data, callback, args) {
        const callObj = rpc_call_struct(method, data);
        // 注册调用ID，等待返回结果
        this.callIds = this.callIds || {};
        this.callIds[callObj.id] = {
            id: callObj.id,
            method: callback,
            params: args || {},
        };
        try {
            if (this.sock == null) {
                this.Connect({
                    ready: sock => {
                        send_message(sock, callObj);
                    },
                });
            } else {
                send_message(this.sock, callObj);
            }
        } catch (error) {
            delete this.callIds[callObj.id];
        }
    }
    Connect(option) {
        const options = option || {};
        const tmp = options.binaryType;
        if (tmp === "arraybuffer" || tmp === "blob") {
            this.binaryType = tmp;
        }
        const binaryType = this.binaryType || "blob";
        if (this.sock == null) {
            try {
                if (window["WebSocket"]) {
                    this.sock = new WebSocket(this.addr);
                    const rev = evt => {
                        this.sock.removeEventListener('message', rev);
                        receive_message(this.sock, evt.data, msg => {
                            this.sock.addEventListener('message', rev);
                            const msgObj = JSON.parse(msg);
                            if (msgObj.action == -0xFF) {
                                ///调用本地服务
                                ///调用本地服务
                                const rpcObj = this.rpcObj || {}
                                // shop.test 测试服务用.号分开
                                const svr_mtd = msgObj.method.split('.')
                                const svr = svr_mtd[0]
                                const mtd = svr_mtd[1]
                                const svrRpcObj = rpcObj[svr] || {}
                                const rpcMethod = svrRpcObj[mtd]
                                if (isFunction(rpcMethod)) {
                                    const rpc = rpcMethod.call(null, msgObj.data);
                                    if (rpc) {
                                        this.Send({
                                            id: msgObj.id,
                                            action: -0xFE,
                                            data: rpc,
                                        });
                                    }
                                    return
                                }
                            }
                            // 调用回调函数
                            const callIds = this.callIds || [];
                            const callId = callIds[msgObj.id];
                            if (callId && msgObj.action == -0xFE) {
                                delete this.callIds[msgObj.id];
                                if (isFunction(callId.method)) {
                                    callId.method.call(null, msgObj.data, callId.params, msg);
                                    return;
                                }
                            }

                            this.listeners.onmessage.map(res => {
                                res.call(this, msgObj);
                                return res;
                            });
                        });
                    };
                    // console.log('binaryType', binaryType, binaryType === "arraybuffer" ? "arraybuffer" : "blob")
                    // 确保只有 ”arraybuffer“ 或 ”blob“
                    this.sock.binaryType = binaryType === "arraybuffer" ? "arraybuffer" : "blob";
                    this.sock.addEventListener('open', (event) => {
                        this.connected = true;
                        if (isFunction(options.ready)) {
                            options.ready(this.sock);
                        }
                        this.listeners.onopen.map(res => {
                            res.call(this, event);
                            return res;
                        });
                    });
                    this.sock.addEventListener('message', rev);
                    this.sock.addEventListener('ping', data => {
                        this.sock.pong();
                    });
                    this.sock.addEventListener('close', event => {
                        this.connected = false;
                        this.sock = null;
                        this.runnging = false;
                        this.listeners.onclose.map(res => {
                            res.call(null, event);
                            return res;
                        });
                    });
                    this.sock.addEventListener('error', event => {
                        this.connected = false;
                        this.listeners.onerror.map(res => {
                            res.call(this, event);
                            return res;
                        });
                    });
                    return this.sock;
                }
            } catch (error) {
                console.log(error);
            }

        }
    }
}







function send_message(wsock, data) {
    if (wsock) {
        const sliceData = sliceMessage(wsock, getMsgId(2), data, 512)
        for (const item of sliceData) {
            const itemStr = JSON.stringify(item)
            const d = isNeedArrayBuffer(wsock) ? new TextEncoder().encode(itemStr) : itemStr
            wsock.send(d);

        }
    }
}



function receive_message(wsock, data, callback) {
    try {
        const rst = [];
        // const dd = [];
        const f = JSON.parse(data)
        rst.push(isNeedArrayBuffer(wsock) ? f.d : Base64.decode(f.d))
        // dd.push(f.d)
        if (f.s == rst.join('').length) {
            if (isFunction(callback)) {
                callback(rst.join(''))
            }
            return
        }
        const rev = evt => {
            try {
                const msg = JSON.parse(evt.data)
                if (msg.n == f.n) {
                    // msg.D 是 base64 编码,需要转成 字符串
                    rst.push(Base64.decode(msg.d))
                    if (f.t == msg.i + 1 && f.s == rst.join('').length) {
                        wsock.removeEventListener('message', rev)
                        if (isFunction(callback)) {
                            callback(rst.join(''))
                        }
                    }
                }
            } catch (error) {
                console.log(error)
            }
        }
        if (!data) return
        if (wsock) {
            wsock.addEventListener('message', rev)
        }
    } catch (error) {
        console.log(error)
    }
}

function getMsgId(n = 2) {
    return Math.random().toString(36).substring(2, 2 + n)
}
/**
 * 是否需要ArrayBuffer
 * @param wsock
 * @returns {boolean}
 */
function isNeedArrayBuffer(wsock) {
    return wsock.binaryType == "arraybuffer"
}

/**
 * 切片消息
 * @param wsock
 * @param name
 * @param data
 * @param len
 * @returns {*[]}
 */
function sliceMessage(wsock, name, data, len) {

    if (isObject(data)) {
        data.data = Base64.encode(data.data)
        data = JSON.stringify(data)
    }
    // "arraybuffer" || "blob"
    if (isNeedArrayBuffer(wsock)) data = new TextEncoder().encode(data)

    const totalSize = data.length
    let totalSlice = parseInt(totalSize / len)
    if (totalSize % len != 0) {
        totalSlice++
    }
    const slices = []
    for (let i = 0; i < totalSlice; i++) {
        const start = i * len
        let end = start + len
        end = Math.min(end, totalSize)
        let sData = data.slice(start, end)
        slices.push({
            n: name,
            t: totalSlice,
            i: i,
            s: totalSize,
            d: isNeedArrayBuffer(wsock) ? sData : Base64.encode(sData),
        })
    }
    // console.log('slices', slices)
    return slices
}


/**
 * 可执行涵数
 * @param param
 * @returns {boolean}
 */
function isFunction(param) {
    return Object.prototype.toString.call(param) === '[object Function]';
}
/**
 * 是否为JS对象
 * @param param
 * @returns {boolean}
 */
function isObject(param) {
    return Object.prototype.toString.call(param) === '[object Object]';
}

// [object Uint8Array]
function isUint8Array(param) {
    return Object.prototype.toString.call(param) === '[object Uint8Array]';
}

/**
 * 是否为字符串
 * @param param
 * @returns {boolean}
 */
function isString(param) {
    return Object.prototype.toString.call(param) === '[object String]';
}

var Base64 = {
    _keyStr: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef" +
        "ghijklmnopqrstuvwxyz0123456789+/=",
    encode: function (e) {
        let t = "";
        let n, r, i, s, o, u, a;
        let f = 0;
        e = Base64._utf8_encode(e);
        while (f < e.length) {
            n = e.charCodeAt(f++);
            r = e.charCodeAt(f++);
            i = e.charCodeAt(f++);
            s = n >> 2;
            o = (n & 3) << 4 | r >> 4;
            u = (r & 15) << 2 | i >> 6;
            a = i & 63;
            if (isNaN(r)) {
                u = a = 64
            } else if (isNaN(i)) {
                a = 64
            }
            t = t +
                this._keyStr.charAt(s) +
                this._keyStr.charAt(o) +
                this._keyStr.charAt(u) +
                this._keyStr.charAt(a)
        }
        return t
    },
    decode: function (e) {
        let t = "";
        let n, r, i;
        let s, o, u, a;
        let f = 0;
        e = e.replace(/[^A-Za-z0-9\+\/\=]/g, "");
        while (f < e.length) {
            s = this._keyStr.indexOf(e.charAt(f++));
            o = this._keyStr.indexOf(e.charAt(f++));
            u = this._keyStr.indexOf(e.charAt(f++));
            a = this._keyStr.indexOf(e.charAt(f++));
            n = s << 2 | o >> 4;
            r = (o & 15) << 4 | u >> 2;
            i = (u & 3) << 6 | a;
            t = t + String.fromCharCode(n);
            if (u != 64) {
                t = t + String.fromCharCode(r)
            }
            if (a != 64) {
                t = t + String.fromCharCode(i)
            }
        }
        t = Base64._utf8_decode(t);
        return t
    },
    _utf8_encode: function (e) {
        e = e.replace(/\r\n/g, "\n");
        let t = "";
        for (let n = 0; n < e.length; n++) {
            let r = e.charCodeAt(n);
            if (r < 128) {
                t += String.fromCharCode(r)
            } else if (r > 127 && r < 2048) {
                t +=
                    String.fromCharCode(r >> 6 | 192);
                t +=
                    String.fromCharCode(r & 63 | 128)
            } else {
                t +=
                    String.fromCharCode(r >> 12 | 224);
                t +=
                    String.fromCharCode(r >> 6 & 63 | 128);
                t +=
                    String.fromCharCode(r & 63 | 128)
            }
        }
        return t
    },
    _utf8_decode: function (e) {
        let t = "";
        let n = 0;
        let r = 0;
        let c1 = 0;
        let c2 = 0;
        let c3 = 0;
        while (n < e.length) {
            r = e.charCodeAt(n);
            if (r < 128) {
                t += String.fromCharCode(r);
                n++
            } else if (r > 191 && r < 224) {
                c2 = e.charCodeAt(n + 1);
                t +=
                    String.fromCharCode(
                        (r & 31) << 6 | c2 & 63);
                n += 2
            } else {
                c2 = e.charCodeAt(n + 1);
                c3 = e.charCodeAt(n + 2);
                t +=
                    String.fromCharCode(
                        (r & 15) << 12 |
                        (c2 & 63) << 6 | c3 & 63);
                n += 3
            }
        }
        return t
    }
}


const TLV_TYPE_STRING = 0x01
const TLV_TYPE_JSON = 0x02
const TLV_TYPE_BINARY = 0x03
const TLV_TYPE_INT64 = 0x04
const TLV_TYPE_UINT64 = 0x05
const TLV_TYPE_FLOAT64 = 0x06


// TLV
class TlV {
    constructor(tag, length, crc, value) {
        this.T = tag;      // 1字节标签
        this.L = length;  // 2字节长度(大端序)
        this.C = crc;     // 2字节CRC校验值
        this.V = value;   // 数据内容
    }
}

function NewTLVFromFrame(b) {
    try {
        const [tag, data] = tlv_decode(b);
        const crc = getCRC(data);
        return new TlV(tag, data.length, crc, data);
    } catch (err) {
        throw err;
    }
}

function isTLVFrame(b) {
    if (b.length < 6) return false; // 至少需要6字节头部
    const length = (b[1] << 8) | b[2]; // 大端序解析长度
    if (b.length < 6 + length) return false;
    const crc = b.slice(4, 6);
    const data = b.slice(6, 6 + length);
    return checkCRC(data, crc);
}

function tlv_encode(tag, data) {
    if (data.length > 0xFFFF) throw new Error("Invalid value length");
    const header = new Uint8Array(6 + data.length);
    header[0] = tag; // 标签
    // 大端序写入长度
    header[1] = (data.length >> 8) & 0xFF;
    header[2] = data.length & 0xFF;
    // 计算并写入CRC
    const crc = getCRC(data);
    header.set(crc, 4);
    // 写入数据
    header.set(data, 6);
    return header;
}

function tlv_decode(b) {
    if (b.length < 6) throw new Error("Invalid frame length");
    const length = (b[1] << 8) | b[2];
    if (b.length < 6 + length) throw new Error("Invalid value length");
    const crc = b.slice(4, 6);
    const data = b.slice(6, 6 + length);
    if (!checkCRC(data, crc)) throw new Error("Invalid CRC");
    return [b[0], data];
}
// 假设使用CRC16算法(具体需根据原utils.GetCrC实现调整)
function getCRC(data) {
    let crc = 0xFFFF;
    for (let i = 0; i < data.length; i++) {
        crc ^= data[i] << 8;
        for (let j = 0; j < 8; j++) {
            if (crc & 0x8000) {
                crc = (crc << 1) ^ 0x1021;
            } else {
                crc = crc << 1;
            }
            crc &= 0xFFFF;
        }
    }
    return [(crc >> 8) & 0xFF, crc & 0xFF];
}

function checkCRC(data, crc) {
    const calculated = getCRC(data);
    return calculated[0] === crc[0] && calculated[1] === crc[1];
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
 * 字符串转 TLV 帧
 * @param {string} v - 输入字符串
 * @returns {Uint8Array} TLV 帧
 */
function frameFromString(v) {
  try {
    const data = new TextEncoder().encode(v);
    const frame = tlv_encode(TLV_TYPE_STRING, data);
    return frame;
  } catch (err) {
    return new Uint8Array();
  }
}

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
  return newTLVFromFrame(v);
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