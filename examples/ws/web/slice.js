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