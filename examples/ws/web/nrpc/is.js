
/**
 * 可执行涵数
 * @param param
 * @returns {boolean}
 */
function isFunction(param) {
    return Object.prototype.toString.call(param) === '[object Function]';
}

function isUndefined(param) {
    return Object.prototype.toString.call(param) === '[object Undefined]'
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

if (typeof module !== 'undefined' && module.exports) {
    module.exports = {
        isFunction,
        isUndefined,
        isObject,
        isUint8Array,
        isString,
    };
}