import Base64 from './base64';
import { isObject, isUint8Array, isString, isFunction,isUndefined  } from './is';
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
        id: getMsgId(4),
        protocol: 1,
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
    Call(method, data, callback, error, args) {
        const callObj = rpc_call_struct(method, data);
        // 注册调用ID，等待返回结果
        this.callIds = this.callIds || {};
        this.callIds[callObj.id] = {
            id: callObj.id,
            protocol: 1,
            method: callback,
            params: args || {},
            error: error,
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
                                        // console.log('rpc', rpc, serialize(rpc))
                                        this.Send({
                                            id: msgObj.id,
                                            protocol: 1,
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
                                if (isUndefined(msgObj.data)) {
                                    if (isFunction(callId.error)) {
                                        callId.error.call(null, msgObj.error, callId.params, msg);
                                        return;
                                    }
                                    return
                                }
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
            // console.log('send_message', itemStr)
            const d = isNeedArrayBuffer(wsock) ? new TextEncoder().encode(itemStr) : itemStr
            // console.log('send_message', d)
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


export {
    send_message,
    receive_message,
    sliceMessage,
    SockRpc,
}
