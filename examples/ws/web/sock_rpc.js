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
        // 0: TextMessage, 1: BinaryMessage
        p: 1,
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
       data = Serialize(data);
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
        if (tmp !== "arraybuffer" && tmp !== "blob") {
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
        const dataItem = {
            n: name,
            t: totalSlice,
            i: i,
            s: totalSize,
            d: sData,
            p: 1,
        }
        console.log('dataItem', dataItem)
        slices.push(dataItem)
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


/********************/
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

// 计算头部大小
function get_header_size(lLen, checkCRC) {
  let c = 0x02;
  if (!checkCRC) c = 0;
  return lLen + 1 + c;
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
/************************ */

// 创建空帧
function EmptyFrame(opts = []) {
  try {
    return tlv_encode(0x00, new Uint8Array(), opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从字符串创建帧
function FrameFromString(v, opts = []) {
  try {
    const data = new TextEncoder().encode(v);
    return tlv_encode(TLV_TYPE_STRING, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从JSON对象创建帧
function FrameFromJson(v, opts = []) {
  try {
    const jsonStr = JSON.stringify(v);
    const data = new TextEncoder().encode(jsonStr);
    return tlv_encode(TLV_TYPE_JSON, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从二进制数据创建帧
function FrameFromBinary(v, opts = []) {
  try {
    return tlv_encode(TLV_TYPE_BINARY, v, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从Float64创建帧
function FrameFromFloat64(v, opts = []) {
  try {
    const buf = new ArrayBuffer(8);
    const dv = new DataView(buf);
    dv.setFloat64(0, v, false); // false表示大端序
    const data = new Uint8Array(buf);
    return tlv_encode(TLV_TYPE_FLOAT64, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从Int64创建帧
function FrameFromInt64(v, opts = []) {
  try {
    const buf = new ArrayBuffer(8);
    const dv = new DataView(buf);
    dv.setBigInt64(0, BigInt(v), false); // 使用BigInt处理64位整数
    const data = new Uint8Array(buf);
    return tlv_encode(TLV_TYPE_INT64, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从Byte创建帧
function FrameFromByte(v, opts = []) {
  try {
    const data = new Uint8Array([v]);
    return tlv_encode(TLV_TYPE_BYTE, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 创建Nil帧
function FrameFromNil(opts = []) {
  try {
    return tlv_encode(TLV_TYPE_NIL, new Uint8Array(), opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 从Uint64创建帧
function FrameFromUint64(v, opts = []) {
  try {
    const buf = new ArrayBuffer(8);
    const dv = new DataView(buf);
    dv.setBigUint64(0, BigInt(v), false); // 使用BigInt处理64位无符号整数
    const data = new Uint8Array(buf);
    return tlv_encode(TLV_TYPE_UINT64, data, opts);
  } catch (err) {
    return new Uint8Array();
  }
}

// 字节转Float64
function Bytes2Float64(v) {
  const dv = new DataView(v.buffer);
  return dv.getFloat64(0, false); // false表示大端序
}

// 从帧解码Float64
function FrameToFloat64(v) {
  if (v.length !== 8 + TLVX_HEADER_SIZE) {
    throw ErrInvalidFloat64;
  }
  if (v[0] !== TLV_TYPE_FLOAT64) {
    throw ErrInvalidFloat64Type;
  }
  return Bytes2Float64(v.subarray(TLVX_HEADER_SIZE));
}

// 字节转Int64
function Bytes2Int64(v) {
  const dv = new DataView(v.buffer);
  return dv.getBigInt64(0, false); // 使用BigInt处理64位整数
}

// 从帧解码Int64
function FrameToInt64(v) {
  if (v.length !== 8 + TLVX_HEADER_SIZE) {
    throw ErrInvalidInt64;
  }
  if (v[0] !== TLV_TYPE_INT64) {
    throw ErrInvalidInt64Type;
  }
  return Bytes2Int64(v.subarray(TLVX_HEADER_SIZE));
}

// 字节转Uint64
function Bytes2Uint64(v) {
  const dv = new DataView(v.buffer);
  return dv.getBigUint64(0, false); // 使用BigInt处理64位无符号整数
}

// 从帧解码Uint64
function FrameToUint64(v) {
  if (v.length !== 8 + TLVX_HEADER_SIZE) {
    throw ErrInvalidUint64;
  }
  if (v[0] !== TLV_TYPE_UINT64) {
    throw ErrInvalidUint64Type;
  }
  return Bytes2Uint64(v.subarray(TLVX_HEADER_SIZE));
}

// 从帧解码为对象
function FrameToStruct(v) {
  if (!v || v.length < TLVX_HEADER_SIZE) {
    throw ErrInvalidValueLength;
  }
  if (v[0] !== TLV_TYPE_JSON) {
    throw ErrInvalidStructType;
  }
  const [, data] = tlv_decode(v);
  return JSON.parse(new TextDecoder().decode(data));
}

// 从帧解码为二进制
function FrameToBin(v) {
  if (!v || v.length < TLVX_HEADER_SIZE) {
    throw ErrInvalidValueLength;
  }
  if (v[0] !== TLV_TYPE_BINARY) {
    throw ErrInvalidBinType;
  }
  const [, data] = tlv_decode(v);
  return data;
}
// 反序列化
function Deserialize(v) {
  if (!v || v.length < TLVX_HEADER_MIN_SIZE) {
    throw ErrInvalidValueLength;
  }
  return NewTLVFromFrame(v);
}

// 序列化
function Serialize(v) {
  if (v === null || v === undefined) {
    return new Uint8Array([TLV_TYPE_NIL, 0]);
  }

  switch (typeof v) {
    case 'number':
      if (Number.isInteger(v)) {
        return FrameFromInt64(BigInt(v));
      } else {
        return FrameFromFloat64(v);
      }
    case 'string':
      return FrameFromString(v);
    case 'boolean':
      return FrameFromInt64(BigInt(v ? 1 : 0));
    case 'object':
      if (v instanceof Uint8Array) {
        return FrameFromBinary(v);
      } else if (Array.isArray(v)) {
        return FrameFromJson(v);
      } else if (v instanceof BigInt) {
        if (v < 0) {
          return FrameFromInt64(v);
        } else {
          return FrameFromUint64(v);
        }
      } else {
        return FrameFromJson(v);
      }
    default:
      return FrameFromJson(v);
  }
}

// 默认编码器
function DefaultEncoder(v) {
  try {
    return Serialize(v);
  } catch (err) {
    return new Uint8Array();
  }
}

// 默认解码器
function DefaultDecoder(data) {
  if (data.length === 0) {
    return null;
  }
  if (data.length < TLVX_HEADER_MIN_SIZE) {
    throw ErrInvalidValueLength;
  }
  const tlv = Deserialize(data);
  return tlv.Value();
}