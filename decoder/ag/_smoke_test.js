const ag = require('./ag.js');
console.log('Module loaded OK. Exported keys:', Object.keys(ag).length);
console.log('ArgumentHeaderSize =', ag.ArgumentHeaderSize);
console.log('Magic =', ag.ArgumentMagic1.toString(16), ag.ArgumentMagic2.toString(16));

function bytesToHex(b) {
  return Array.from(b).map(x => x.toString(16).padStart(2, '0')).join(' ');
}

function assert(cond, msg) {
  if (!cond) { console.error('FAIL:', msg); process.exit(1); }
  console.log('PASS:', msg);
}

// --- nil ---
let raw = ag.Encode(null);
assert(ag.IsArgument(raw), 'nil IsArgument');
assert(raw.length === ag.ArgumentHeaderSize, 'nil frame len = 5');
assert(raw[2] === ag.ArgumentTypeNil, 'nil tag');
assert(ag.Decode(raw) === null, 'nil roundtrip');

// --- bool ---
raw = ag.Encode(true);
assert(ag.Decode(raw) === true, 'bool true rt');
raw = ag.Encode(false);
assert(ag.Decode(raw) === false, 'bool false rt');

// --- int ---
raw = ag.Encode(-42);
assert(ag.IsArgument(raw), 'int IsArgument');
let v = ag.Decode(raw);
assert(v === -42, `int -42 rt got=${v}`);

raw = ag.Encode(0);
assert(ag.Decode(raw) === 0, 'int 0 rt');

raw = ag.Encode(ag.AsInt8(-8));
assert(raw[2] === ag.ArgumentTypeInt8, 'AsInt8 tag');
assert(ag.Decode(raw) === -8, 'int8 -8 rt');

raw = ag.Encode(ag.AsInt16(-16));
assert(ag.Decode(raw) === -16, 'int16 -16 rt');

raw = ag.Encode(ag.AsInt32(-32));
assert(ag.Decode(raw) === -32, 'int32 -32 rt');

// --- uint ---
raw = ag.Encode(ag.AsUint8(255));
assert(ag.Decode(raw) === 255, 'uint8 255 rt');

raw = ag.Encode(ag.AsUint16(65535));
assert(ag.Decode(raw) === 65535, 'uint16 65535 rt');

raw = ag.Encode(ag.AsUint32(0xFFFFFFFF));
v = ag.Decode(raw);
assert(v === 0xFFFFFFFF, `uint32 0xFFFFFFFF rt got=${v}`);

// --- float ---
raw = ag.Encode(ag.AsFloat32(3.14));
assert(raw[2] === ag.ArgumentTypeFloat32, 'AsFloat32 tag');
v = ag.Decode(raw);
assert(Math.abs(v - 3.14) < 1e-6, `float32 3.14 rt got=${v}`);

raw = ag.Encode(3.141592653589793);
assert(raw[2] === ag.ArgumentTypeFloat64, 'double tag by default');
v = ag.Decode(raw);
assert(v === 3.141592653589793, `float64 exact rt got=${v}`);

// --- complex ---
raw = ag.Encode(ag.AsComplex64(1, 2));
assert(raw[2] === ag.ArgumentTypeComplex64, 'complex64 tag');
let c = ag.Decode(raw);
assert(c.real === 1 && c.imag === 2, `complex64 rt got=${JSON.stringify(c)}`);

raw = ag.Encode({ real: 3.0, imag: 4.0 });
assert(raw[2] === ag.ArgumentTypeComplex128, 'complex128 auto-detect');
c = ag.Decode(raw);
assert(c.real === 3 && c.imag === 4, `complex128 rt got=${JSON.stringify(c)}`);

// --- string ---
raw = ag.Encode('Hello, JS!');
assert(ag.Decode(raw) === 'Hello, JS!', 'string ascii rt');
raw = ag.Encode('中文ab1234`');
assert(ag.Decode(raw) === '中文ab1234`', 'string utf8 rt');

// --- bytes ---
raw = ag.Encode(new Uint8Array([1, 2, 3]));
assert(raw[2] === ag.ArgumentTypeBytes, 'bytes tag');
let b = ag.Decode(raw);
assert(b instanceof Uint8Array && b.length === 3 && b[0] === 1 && b[1] === 2 && b[2] === 3, 'bytes rt');
// 修改解码结果，验证独立性
b[0] = 99;
let b2 = ag.Decode(raw);
assert(b2[0] === 1, 'bytes decode independent copy');

// --- composite → json string ---
raw = ag.Encode([1, 2, -3, 4, 5]);
assert(raw[2] === ag.ArgumentTypeString, 'slice→string tag');
let s = ag.Decode(raw);
let arr = JSON.parse(s);
assert(JSON.stringify(arr) === JSON.stringify([1, 2, -3, 4, 5]), `slice json rt got=${s}`);

raw = ag.Encode({ a: 1, b: 2, c: 3 });
s = ag.Decode(raw);
let m = JSON.parse(s);
assert(m.a === 1 && m.b === 2 && m.c === 3, `map json rt got=${s}`);

raw = ag.Encode({ c: '中文ab1234`' });
s = ag.Decode(raw);
let o = JSON.parse(s);
assert(o.c === '中文ab1234`', `struct json rt got=${s}`);

// --- Data / Value ---
raw = ag.Encode('ABC');
let d = ag.Data(raw);
assert(String.fromCharCode(d[0], d[1], d[2]) === 'ABC', 'Data func');
assert(ag.Value(raw).length === 3, 'Value alias');

// --- Validate ---
assert(ag.Validate(new Uint8Array([0x3A, 0x70, 0x02, 0, 1, 1])) === null, 'Validate good');
assert(ag.Validate(new Uint8Array([1, 2, 3])) === ag.ErrAgTooShort, 'Validate too short');
assert(ag.Validate(new Uint8Array([0, 0, 0, 0, 0])) === ag.ErrAgBadMagic, 'Validate bad magic');

// --- Decode invalid ---
let threw = false;
try { ag.Decode(new Uint8Array([1, 2, 3])); } catch (e) { if (e === ag.ErrAgInvalidHeader) threw = true; }
assert(threw, 'Decode invalid throws ErrAgInvalidHeader');

// --- ErrAgDataTooLarge ---
threw = false;
try { ag.Encode(new Uint8Array(ag.ArgumentMaxDataSize + 1)); } catch (e) { if (e === ag.ErrAgDataTooLarge) threw = true; }
assert(threw, 'Encode oversized throws ErrAgDataTooLarge');
// 边界正好 65536（与 Go 版一致：不抛错，但 LEN 会溢位）
raw = ag.Encode(new Uint8Array(ag.ArgumentMaxDataSize));
// Go 版只检查 Encode 不返回 error；这里同理通过即可
assert(true, 'boundary 65536 Encode ok (no throw)');

// --- typeName ---
assert(ag.typeName(ag.ArgumentTypeInt64) === 'int64', 'typeName int64');
assert(ag.typeName(ag.ArgumentTypeString) === 'string', 'typeName string');
assert(ag.typeName(99).startsWith('unknown'), 'typeName unknown');

// --- int boundary 负数不丢失符号 ---
raw = ag.Encode(ag.AsInt8(-128));
assert(ag.Decode(raw) === -128, 'int8.min -128 rt');
raw = ag.Encode(ag.AsInt8(127));
assert(ag.Decode(raw) === 127, 'int8.max 127 rt');
raw = ag.Encode(ag.AsInt8(-1));
v = ag.Decode(raw);
assert(v === -1, `int8 -1 rt got=${v}`);

raw = ag.Encode(ag.AsInt16(-32768));
assert(ag.Decode(raw) === -32768, 'int16.min rt');
raw = ag.Encode(ag.AsInt16(32767));
assert(ag.Decode(raw) === 32767, 'int16.max rt');

// --- 前导 0 压缩验证 ---
raw = ag.Encode(ag.AsUint64(0));
// 0 → 压缩后应为 1 字节 value
let valLen = (raw[3] << 8) | raw[4];
assert(valLen === 1, `uint64(0) value len=${valLen} want 1`);

raw = ag.Encode(ag.AsUint64(256));
valLen = (raw[3] << 8) | raw[4];
assert(valLen === 2, `uint64(256) value len=${valLen} want 2`);

console.log('\nAll smoke tests passed!');
