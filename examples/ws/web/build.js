/* ============================================================
 * build.js: 合并 index_v3.html 加载顺序的 5 个浏览器脚本 → sloth_v3_min.js
 *
 *  输入顺序（和 index_v3.html L17-L21 保持严格一致）：
 *      1. tools.js            (crc16 查表 / Base64 / Uint64 读写等基础工具)
 *      2. slice.js            (DataSlice 分片聚合二进制)
 *      3. ag.js               (AG 参数帧 Encode/Decode)
 *      4. fn.js               (FN 传输帧 Magic+Action+ID+Length+Data)
 *      5. sock_rpc_v3.js      (V3 SockRpc 客户端：WS/分片/TLV CRC/Call/Bind/重连)
 *
 *  输出：
 *      - sloth_v3_min.js        默认输出：terser 压缩（若可用）；无 terser 就直接合并 + 注释
 *      - sloth_v3_bundle.js     始终输出：合并后的"未压缩全量版本"（带 banner，方便调试）
 *
 *  用法：
 *      cd examples/ws/web
 *      node build.js                       # 默认输出到当前目录 examples/ws/web/
 *      node build.js --out-dir ../../dist  # 自定义输出目录（会自动 mkdir）
 *      node build.js --no-minify           # 跳过敏捷压缩，只生成 bundle
 *      node build.js --install-terser      # 本脚本本地临时 npm i terser 后再跑（无需手动改全局）
 *
 *  注意：
 *      · 本脚本完全"零默认依赖"——不要求全局 terser / 不要求 package.json。
 *        只有当你传 --install-terser 时，才会在 examples/ws/web 目录临时 npm i terser。
 *      · 合并顺序严格和 index_v3.html 保持一致，避免依赖加载错乱。
 *      · 所有输入脚本都会用 UTF-8 读取；输出也为 UTF-8（无 BOM）。
 * ============================================================ */
'use strict';

const fs   = require('fs');
const path = require('path');
const cp   = require('child_process');

/* -------- CLI 解析 -------- */
const args = process.argv.slice(2);
function flag(name) { return args.indexOf(name) !== -1; }
function pick(name, dft) {
    const i = args.indexOf(name);
    return (i >= 0 && i + 1 < args.length) ? args[i + 1] : dft;
}

const WEB_DIR    = __dirname;
const OUT_DIR    = path.resolve(WEB_DIR, pick('--out-dir', '.'));
const NO_MINIFY  = flag('--no-minify');
const INSTALL_T  = flag('--install-terser');

const INPUTS = [
    'tools.js',
    'slice.js',
    'ag.js',
    'fn.js',
    'sock_rpc_v3.js',
];
const BANNER = [
    '/*!',
    ' * sloth_v3_bundle.js  /  sloth_v3_min.js',
    ' *',
    ' * Concatenated & built from:',
    INPUTS.map((f, i) => ` *   ${i + 1}. ${f}`).join('\n'),
    ' *',
    ' * Build order exactly matches: examples/ws/web/index_v3.html L17-L21',
    ' * Generated at: ' + new Date().toISOString(),
    ' */',
    '',
].join('\n');

/* -------- 工具 -------- */
function ensureDir(d) {
    if (!fs.existsSync(d)) fs.mkdirSync(d, { recursive: true });
}
function readUtf8(p) {
    if (!fs.existsSync(p)) {
        console.error('[build] ❌ 缺少输入文件: ' + p);
        process.exit(1);
    }
    return fs.readFileSync(p, 'utf8').replace(/^\uFEFF/, '');
}
function writeUtf8(p, s) {
    ensureDir(path.dirname(p));
    fs.writeFileSync(p, s, 'utf8');
    const kb = (Buffer.byteLength(s, 'utf8') / 1024).toFixed(2);
    console.log(`[build] ✅ wrote ${path.relative(process.cwd(), p) || p}  (${kb} KB)`);
}

/* ============================================================
 * 【打包"全局提升"白名单】
 *
 *  为解决上一版"每段包 IIFE 导致 tools.js 声明的 zeroExtendN/getCRC 等函数
 *  困在自己的 IIFE 作用域内、ag.js/slice.js/fn.js 按全局名引用时
 *  ReferenceError"的问题，本版 build 策略改为：
 *
 *    ① 所有 5 个 source 不做分段 IIFE，直接顺序拼到**同一个大 IIFE** 内
 *       （相当于它们共享一个 <script> 的顶级作用域，完全等价于按顺序
 *        写 5 个独立 <script> 标签时，顶层 const/function/var 的可见性）。
 *
 *    ② 大 IIFE 末尾**显式**地把"原本设计暴露给浏览器全局"的符号
 *       挂到 window / self / globalThis 上，避免 terser 压缩时把这些
 *       顶层声明当"内部变量"被 mangler 改名或被 unused 删掉。
 *
 *  白名单来源：
 *      · tools.js    → 手工扫：zeroExtendN / zeroExtend2 / 4 / 8byte /
 *                        crc16_h / crc16_l / getCRC / Base64 /
 *                        writeUint64BE / readUint64BE / IsComplete
 *      · slice.js    → 原导出 (TextMessage / BinaryMessage 等 6 个 类)
 *                        + slice / DataSlice 等辅助函数
 *      · ag.js       → 只在 window.AG 上导出，但 ag.js 内部自己
 *                        **按裸名直接引用 zeroExtendN**，所以 zeroExtendN
 *                        必须在共享作用域可见（这一步就是 ① 带来的效果）
 *      · fn.js       → fn.IsFn / BuildCall / BuildReply / ParseData /
 *                        IsComplete / ACTION_* 常量
 *      · sock_rpc_v3 → SockRpcV3 / TlvJson / TlvValue /
 *                        TlvValueAsText / TlvValueAsJson
 *
 *  注意：reserved 列表必须和 IIFE 尾部 window.xxx = xxx 的赋值一致，
 *        否则 mangler 会把共享作用域里的 const xxx 改名为 a/b/c，然后
 *        window.xxx = a 导致全局名正确但"原符号名"丢失。
 * ============================================================ */
const GLOBAL_EXPORTS = [
    // ---- tools.js 顶层（const/function 全量） ----
    '_HAS_BIGINT',
    'encode_bytes',
    'decode_bytes',
    'decode_string',
    '_toBig',
    '_bitAnd',
    '_lshift',
    '_urshift',
    'zeroExtendN',
    'zeroExtend2byte',
    'zeroExtend4byte',
    'zeroExtend8byte',
    'int_to_byte',
    'bi_holder',
    '_intToByteImpl',
    'uint_to_byte',
    'to_uint64',
    'to_int64',
    'narrowInt8',
    'narrowInt16',
    'narrowInt32',
    'narrowUint8',
    'narrowUint16',
    'narrowUint32',
    'jsonMarshalFallback',
    '_serialize',
    'writeUint64BE',
    'readUint64BE',
    'crc16_h',
    'crc16_l',
    'getCRC',

    // ---- slice.js 顶层（注意：GetCrC 在原 slice.js 未定义，但原 module.exports
    //                         写了 GetCrC，我们这里也一并列出来，避免 terser 报错；
    //                         另：SliceMessage / newSlice* / SliceTypes / SliceSize /
    //                         class Slice / function Slice 在 slice.js 内实际不存在，
    //                         原 module.exports 也没导出它们，尾部 window 提升时
    //                         typeof 检测会自动跳过。）----
    'TextMessage',
    'BinaryMessage',
    'LongMessage',
    'CRC',
    'DataSlice',
    'newOption',
    'get_header_size',
    'serialize',
    'Option',
    'IsComplete',
    'CheckCRC',        // 同名函数有两个，JS 的 var/function 提升会让后者覆盖前者
    'Encode',
    'Decode',
    'GetCrC',          // 兼容 slice.js 原 module.exports 的写法（未定义但在导出里写了）
    'SliceMessage',
    'Slice',
    'newSliceText',
    'newSliceBinary',
    'decodeSliceText',
    'decodeSliceBinary',
    'concatSliceBinary',
    'SliceTypes',
    'SliceSize',
    // （如果未来 slice.js 新增 Slice 类 / 分片辅助函数，把对应裸名加这里即可）

    // ---- fn.js 顶层 ----
    'FnMagic1',
    'FnMagic2',
    'FnHeaderSize',
    'FnMaxDataSize',
    'FnError',
    'ErrFnTooShort',
    'ErrFnBadMagic',
    'ErrFnLengthMismatch',
    'ErrFnDataTooLarge',
    'ErrFnNilFrame',
    'ErrFnInvalidFrame',
    'ErrFnInvalidAction',
    'FnFrame',
    'FnHeader',
    'IsFn',
    'toUint8Array',
    'EncodeFn',
    'Encode',
    '_encodeInternal',
    'DecodeFn',
    'Decode',
    '_decodeInternal',
    'Id',
    'Action',
    'Data',
    'ValidateFn',
    'ParseFnHeader',
    'wrapError',
    '_exports',
    // fn.js 浏览器端把 _exports 挂到 window.Fn；为保持"裸名也可用"，再列出所有 ACTION：
    'ACTION_CALL',
    'ACTION_REPLY_SUCCESS',
    'ACTION_REPLY_ERROR',
    'ACTION_BROADCAST',
    'FN_HEADER_SIZE',
    'FN_MAGIC_1',
    'FN_MAGIC_2',
    'FN',
    'BuildCall',
    'BuildReply',
    'BuildBroadcast',
    'ParseData',

    // ---- ag.js 顶层（const/function/导出对象）----
    'AGExports',
    'AG',
    // （ag.js 内部的顶层 function/const 这里不列：ag.js 内部自己的零扩展工具等都通过 AGExports
    //  对外暴露，共享作用域下默认就互相可见。此处仅保留导出对象。）

    // ---- sock_rpc_v3.js 顶层（类/工具）----
    'SockRpcV3',
    'TlvJson',
    'TlvValue',
    'TlvValueAsText',
    'TlvValueAsJson',
];

function concatAll() {
    const pieces = [
        BANNER,
        '(function () {\n',
        '"use strict";\n',
    ];
    for (const name of INPUTS) {
        const full = path.join(WEB_DIR, name);
        let src  = readUtf8(full);

        /* ============================================================
         * 每段 source 单独做一轮"导出语句的运行时安全化补丁"，
         * 避免原脚本仅在 CommonJS / 浏览器全局里用短路写法，在"
         * Node 调 require() 验证 bundle 时"走到错误分支"导致
         * ReferenceError 或模块导出污染。
         *
         * 已发现的补丁点：
         *  1) slice.js module.exports 里写了「GetCrC」，但源码本身
         *     没有定义这个标识符（可能是历史残留 getCRC 命名错拼）。
         *     → 在它被引用之前做：typeof GetCrC === 'undefined' && (GetCrC = getCRC);
         *     让浏览器/node 都能正常运行。
         *  2) slice.js/fn.js/ag.js 的 module.exports / window.Fn /
         *     window.AG 分支，保持原样即可：浏览器端自然走 window，
         *     Node require 走 module.exports；尾部 window/global 提升
         *     还会再做一轮兜底把 裸名 也挂到 __root__。
         * ============================================================ */
        if (name === 'slice.js') {
            src = '/* patch: slice.js 错误引用 GetCrC，自动别名 getCRC */\n' +
                  'if (typeof GetCrC === "undefined" && typeof getCRC !== "undefined") { var GetCrC = getCRC; }\n' +
                  src;
        }

        pieces.push(
            `\n/* ============================================================\n` +
            ` * source: ${name}\n` +
            ` * ============================================================ */\n` +
            // 把每段源码里开头的 'use strict' 去掉，避免重复
            (src.replace(/^\s*(['"])use strict\1;?\s*/, '')) +
            '\n'
        );
    }
    pieces.push('\n/* ============================================================\n');
    pieces.push(' * 共享 IIFE 尾部：把白名单显式挂到全局（兼容 浏览器 / Worker / 其他宿主）\n');
    pieces.push(' * ============================================================ */\n');
    pieces.push('  var __root__ = (typeof globalThis !== "undefined") ? globalThis\n');
    pieces.push('              : (typeof window     !== "undefined") ? window\n');
    pieces.push('              : (typeof self       !== "undefined") ? self\n');
    pieces.push('              : (typeof global     !== "undefined") ? global\n');
    pieces.push('              : Function("return this")();\n\n');
    for (const sym of GLOBAL_EXPORTS) {
        pieces.push(
            `  try { if (typeof ${sym} !== "undefined") __root__["${sym}"] = ${sym}; } catch (_e) {}\n`
        );
    }
    pieces.push('\n})();\n');
    return pieces.join('');
}

/* -------- terser: 先尝试 require，再按需 npm install 到当前目录的 node_modules -------- */
function tryRequireTerser() {
    try { return require('terser'); } catch (_e) { /* ignore */ }
    // 再试试当前目录下的 node_modules（build 时可能已经装了）
    const local = path.join(WEB_DIR, 'node_modules', 'terser');
    if (fs.existsSync(local)) {
        try { return require(local); } catch (_e2) { /* ignore */ }
    }
    return null;
}

function installTerserSync() {
    console.log('[build] 📦 npm install terser (into ' + WEB_DIR + ') ...');
    cp.execSync('npm install --no-save terser', { cwd: WEB_DIR, stdio: 'inherit' });
}

async function minifyWithTerser(code) {
    const terser = tryRequireTerser();
    if (!terser) return { skipped: true, reason: 'terser not available' };
    try {
        const r = await terser.minify(code, {
            compress: {
                passes: 2,
                drop_console: false,   // 保留 console.log：用户 index_v3 有大量调试日志
                dead_code: true,
                // 关 unused 主开关，避免把"仅被尾部 window.xxx=xxx 读取"的顶层声明当未用删
                unused: false,
                typeofs: false,        // 兼容老浏览器：typeof x 不压缩
                toplevel: false,       // 不删/不折叠外层大 IIFE 内的顶级声明
            },
            mangle:   {
                safari10: true,
                toplevel: false,
                // 保留所有导出白名单的裸名：
                //   作用域里的 const zeroExtendN / function getCRC / class SockRpcV3
                //   都会在尾部 window[S]=S 被读取，一旦 mangler 改名 S→a 就变成全局符号没了
                reserved: GLOBAL_EXPORTS.slice(),
            },
            // 不要改 toplevel "命名顺序"、不要处理 source 的原始命名（保留原符号调试友好）
            nameCache: null,
            // 保持原始模块语义：我们给 IIFE 包了一层，所以 module: false
            module: false,
            output:   {
                // 保留 banner（压缩版也能一眼看到来源）
                preamble: '/*! sloth_v3_min.js — built from tools/slice/ag/fn/sock_rpc_v3 */\n',
                comments: false,
                // 换行长度 cap，便于调试时断点定位
                max_line_len: 32766,
            },
            sourceMap: false,
            // 必须让 "共享顶级作用域" 参与压缩/mangle（否则 IIFE 里的变量名会被全量 mangling，
            // 破坏 reserved 白名单对齐）
            toplevel: false,
        });
        if (r && r.error) return { skipped: true, reason: String(r.error) };
        return { code: (r && r.code) ? r.code : '' };
    } catch (e) {
        return { skipped: true, reason: String(e && e.message || e) };
    }
}

/* -------- 主流程 -------- */
(async function main() {
    ensureDir(OUT_DIR);
    if (INSTALL_T) installTerserSync();

    const bundle = concatAll();
    const bundlePath = path.join(OUT_DIR, 'sloth_v3_bundle.js');
    writeUtf8(bundlePath, bundle);

    if (NO_MINIFY) {
        console.log('[build] ⏭️  --no-minify: 跳过压缩输出');
    } else {
        let terser = tryRequireTerser();
        if (!terser) {
            // 没传 --install-terser 也没 terser：给出友好提示，不报错直接输出 bundle 即可
            console.warn(
                '[build] ⚠️  未检测到 terser。压缩步骤跳过（仅输出 sloth_v3_bundle.js）。\n' +
                '         如要生成 sloth_v3_min.js，可再跑一次：\n' +
                '             node build.js --install-terser'
            );
        } else {
            const out = await minifyWithTerser(bundle);
            if (out.skipped) {
                console.warn('[build] ⚠️  terser 压缩失败，跳过 min 输出: ' + out.reason);
            } else {
                const minPath = path.join(OUT_DIR, 'sloth_v3_min.js');
                writeUtf8(minPath, out.code);

                // 额外输出一份 sloth_v3_min.js 的语法校验（避免压缩器产生错误代码）
                try {
                    const nodeBin = process.execPath;
                    cp.execSync(`"${nodeBin}" --check "${minPath}"`, { stdio: 'pipe' });
                    console.log('[build] ✅ sloth_v3_min.js 语法校验通过 (node --check)');
                } catch (e) {
                    console.error('[build] ❌ sloth_v3_min.js 语法校验失败：' + String(e && e.message || e));
                    process.exit(2);
                }
            }
        }
    }

    console.log('[build] ✅ DONE');
})().catch(e => {
    console.error('[build] ❌ unexpected error:', e && e.stack ? e.stack : e);
    process.exit(99);
});
