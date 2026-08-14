const fs = require('fs');
const os = require('os');
const path = require('path');
const child_process = require('child_process');

const slicePath = path.join(__dirname, 'slice.js');
const { DataSlice, Encode, Decode, GetCrC, CheckCRC, CRC, TextMessage, BinaryMessage, LongMessage, newOption, CheckCRC: CheckCRCOpt } = require(slicePath);

function toHex(u8) {
    return Buffer.from(u8.buffer, u8.byteOffset, u8.byteLength).toString('hex');
}
function fromHex(h) {
    const b = Buffer.from(h, 'hex');
    return new Uint8Array(b.buffer, b.byteOffset, b.byteLength);
}

// ---------- Part 1: Go encode -> Node decode ----------
let goJson = null;
try {
    const tmpDir = __dirname;
    const goOut = child_process.execSync('go run cross_verify_main.go', { cwd: tmpDir, stdio: ['ignore', 'pipe', 'pipe'] });
    goJson = JSON.parse(goOut.toString().trim());
    console.log('Go cases:', goJson.map(c => c.name).join(', '));
} catch (e) {
    console.error('FAIL: run go cross_verify_main.go:', e.stderr ? e.stderr.toString() : e.message);
    process.exit(1);
}

let failed = 0;
for (const c of goJson) {
    const raw = fromHex(c.hex);
    let s = null;
    try {
        s = Decode(raw);
    } catch (e) {
        console.error(`[GoEncode->JSDecode] FAIL ${c.name}: Decode threw: ${e.message}`);
        failed++;
        continue;
    }
    const mismatches = [];
    if ((s.P & 0xFF) !== c.p) mismatches.push(`P: js=${s.P} go=${c.p}`);
    if (s.N !== c.n) mismatches.push(`N: js=${JSON.stringify(s.N)} go=${JSON.stringify(c.n)}`);
    if ((s.T & 0xFF) !== c.t) mismatches.push(`T: js=${s.T} go=${c.t}`);
    if ((s.I & 0xFF) !== c.i) mismatches.push(`I: js=${s.I} go=${c.i}`);
    if ((s.S >>> 0) !== (c.s >>> 0)) mismatches.push(`S: js=${s.S} go=${c.s}`);
    if (s.D.length !== c.dLen) mismatches.push(`D.len: js=${s.D.length} go=${c.dLen}`);
    const dh = toHex(s.D.subarray(0, Math.min(8, s.D.length)));
    if (dh !== c.dHead) mismatches.push(`D.head: js=${dh} go=${c.dHead}`);
    const dt = toHex(s.D.subarray(Math.max(0, s.D.length - 8)));
    if (dt !== c.dHeadLast) mismatches.push(`D.last: js=${dt} go=${c.dHeadLast}`);
    if (mismatches.length) {
        console.error(`[GoEncode->JSDecode] FAIL ${c.name}: ` + mismatches.join('; '));
        failed++;
    } else {
        console.log(`[GoEncode->JSDecode] OK   ${c.name} P=${c.p} N=${JSON.stringify(c.n)} T=${c.t} I=${c.i} S=${c.s} D.len=${c.dLen}`);
    }
}

// ---------- Part 2: CRC byte order cross check ----------
{
    const sample = new Uint8Array([0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39]);
    const crc = GetCrC(sample);
    console.log(`[CRC check] "123456789" crc bytes = [${crc[0].toString(16).padStart(2, '0')} ${crc[1].toString(16).padStart(2, '0')}]`);
    if (!CheckCRC(sample, crc)) {
        console.error('[CRC check] FAIL self-check');
        failed++;
    }
}

// ---------- Part 3: JS encode -> Go decode ----------
function goDecode(hexStr) {
    const src = path.join(__dirname, 'cross_verify_decode_once.go');
    const script = `
//go:build ignore

package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	frame "github.com/w6xian/sloth/v3/decoder/frame"
)
func main() {
	arg := os.Getenv("CROSS_VERIFY_HEX_FILE")
	var hexStr string
	if arg != "" {
		b, err := os.ReadFile(arg)
		if err != nil { fmt.Println("ERR " + err.Error()); return }
		hexStr = strings.TrimSpace(string(b))
	} else {
		if len(os.Args) < 2 { fmt.Println("ERR missing arg"); return }
		hexStr = os.Args[1]
	}
	raw, _ := hex.DecodeString(hexStr)
	s, err := frame.Decode(raw)
	if err != nil { fmt.Println("ERR " + err.Error()); return }
	head := s.D
	if len(head) > 8 { head = head[:8] }
	tail := s.D
	if len(tail) > 8 { tail = tail[len(tail)-8:] }
	fmt.Printf("P=%d N=%s T=%d I=%d S=%d DLEN=%d DHEAD=%s DTAIL=%s\\n", s.P, s.N, s.T, s.I, s.S, len(s.D), hex.EncodeToString(head), hex.EncodeToString(tail))
}
`;
    fs.writeFileSync(src, script);
    const tmpBin = path.join(__dirname, 'cross_verify_tmp_' + process.pid + '.exe');
    child_process.execSync(`go build -o ${tmpBin} cross_verify_decode_once.go`, { cwd: __dirname, stdio: ['ignore', 'pipe', 'pipe'] });
    // pass hex via temp file to avoid Windows cmd length limit (8191)
    const tmpArg = path.join(__dirname, 'cross_verify_arg_' + process.pid + '.tmp');
    try {
        fs.writeFileSync(tmpArg, hexStr);
        const env = Object.assign({}, process.env, { CROSS_VERIFY_HEX_FILE: tmpArg });
        const out = child_process.execFileSync(tmpBin, [], { cwd: __dirname, stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 200 * 1024 * 1024, env });
        return out.toString().trim();
    } catch (e) {
        const out = (e.stdout ? e.stdout.toString().trim() : '');
        const err = (e.stderr ? e.stderr.toString().trim() : '');
        const status = (typeof e.status === 'number') ? e.status : 'null';
        const sig = (typeof e.signal === 'string') ? e.signal : 'null';
        console.error(`[goDecode debug] status=${status} signal=${sig} stdout.head=${JSON.stringify(out.slice(0, 200))} stderr.head=${JSON.stringify(err.slice(0, 500))}`);
        return out + '\nSTDERR:' + err;
    } finally {
        try { fs.unlinkSync(tmpBin); } catch (_) {}
        try { fs.unlinkSync(tmpArg); } catch (_) {}
    }
}

const jsCases = [
    { name: 'js_short_noCRC', p: TextMessage, n: 'ab', t: 3, i: 1, s: 5, d: 'hello' },
    { name: 'js_short_CRC', p: BinaryMessage, n: 'ab', t: 3, i: 1, s: 5, d: 'hello', crcOpt: true },
    { name: 'js_CRC_inP', p: BinaryMessage | CRC, n: 'ab', t: 3, i: 1, s: 5, d: 'hello' },
    { name: 'js_emptyName', p: TextMessage, n: '', t: 1, i: 0, s: 0, d: '' },
    { name: 'js_oneChar', p: TextMessage, n: 'x', t: 1, i: 0, s: 4, d: new Uint8Array([0x11, 0x22, 0x33, 0x44]) },
    { name: 'js_longNamePrefix', p: TextMessage, n: 'xyz999', t: 10, i: 5, s: 2, d: new Uint8Array([0xDE, 0xAD]) },
    { name: 'js_shortBoundary_65535', p: BinaryMessage, n: 'AB', t: 1, i: 0, s: 0xFFFF, d: new Uint8Array(0xFFFF) },
    { name: 'js_longMessage_65536', p: BinaryMessage, n: 'AB', t: 1, i: 0, s: 0x10000, d: new Uint8Array(0x10000) },
];

for (const c of jsCases) {
    const d = typeof c.d === 'string' ? new TextEncoder().encode(c.d) : c.d;
    const s = new DataSlice(c.p, c.n, c.t, c.i, c.s, d);
    const opts = c.crcOpt ? [() => ({ CheckCRC: false })] : []; // ensure opts array API is not crashing
    let realOpts = [];
    if (c.crcOpt) {
        realOpts = [function(o){ o.CheckCRC = true; }];
    }
    s.S = d.length; // keep consistent with Go: Encode ignores S field, but we store length for Decode compare
    const raw = Encode(s, realOpts);
    const hexStr = toHex(raw);

    // first self decode
    let selfDec = null;
    try { selfDec = Decode(raw); } catch (e) {
        console.error(`[JSEncode] FAIL self-decode ${c.name}: ${e.message}`);
        failed++; continue;
    }
    // call go
    const out = goDecode(hexStr);
    if (out.startsWith('ERR')) {
        console.error(`[JSEncode->GoDecode] FAIL ${c.name}: Go returned: ${out}`);
        failed++; continue;
    }
    // parse P N T I S DLEN DHEAD DTAIL
    const m = /P=(\d+) N=(.*?) T=(\d+) I=(\d+) S=(\d+) DLEN=(\d+) DHEAD=(\w*) DTAIL=(\w*)/.exec(out);
    if (!m) {
        console.error(`[JSEncode->GoDecode] FAIL ${c.name}: bad Go output ${out}`);
        failed++; continue;
    }
    const [_, pStr, nStr, tStr, iStr, sStr, dlenStr, dHead, dTail] = m;
    const mismatches = [];
    if ((selfDec.P & 0xFF) !== +pStr) mismatches.push(`P: js=${selfDec.P} go=${pStr}`);
    if (selfDec.N !== nStr) mismatches.push(`N: js=${JSON.stringify(selfDec.N)} go=${JSON.stringify(nStr)}`);
    if ((selfDec.T & 0xFF) !== +tStr) mismatches.push(`T: js=${selfDec.T} go=${tStr}`);
    if ((selfDec.I & 0xFF) !== +iStr) mismatches.push(`I: js=${selfDec.I} go=${iStr}`);
    if ((selfDec.S >>> 0) !== +sStr) mismatches.push(`S: js=${selfDec.S} go=${sStr}`);
    if (selfDec.D.length !== +dlenStr) mismatches.push(`DLEN: js=${selfDec.D.length} go=${dlenStr}`);
    const jdh = toHex(selfDec.D.subarray(0, Math.min(8, selfDec.D.length)));
    if (jdh !== dHead) mismatches.push(`DHEAD: js=${jdh} go=${dHead}`);
    const jdt = toHex(selfDec.D.subarray(Math.max(0, selfDec.D.length - 8)));
    if (jdt !== dTail) mismatches.push(`DTAIL: js=${jdt} go=${dTail}`);
    if (mismatches.length) {
        console.error(`[JSEncode->GoDecode] FAIL ${c.name}: ` + mismatches.join('; '));
        failed++;
    } else {
        console.log(`[JSEncode->GoDecode] OK   ${c.name} P=${pStr} N=${JSON.stringify(nStr)} T=${tStr} I=${iStr} S=${sStr} D.len=${dlenStr}`);
    }
}

// cleanup
try { fs.unlinkSync(path.join(__dirname, 'cross_verify_decode_once.go')); } catch (_) {}

console.log('');
if (failed === 0) {
    console.log('ALL CROSS-VERIFY CASES PASSED');
    process.exit(0);
} else {
    console.error(`FAILED: ${failed} cases`);
    process.exit(1);
}
