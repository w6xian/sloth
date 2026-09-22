// Package ref 用反射把任意 Go 对象的方法注册成"可按名字调用"的服务，
// 并在调用时完成参数构造与方法分发。
//
// 它是 sloth 服务端"注册一个业务对象就能被远端调用"的底层机制：
// sloth.RegisterService(...) 内部走的就是 ref.Register。
//
// 谁需要直接用它：
//   - 只用 sloth：不需要，走根包的 RegisterService / Call 即可；
//   - 自研框架或要做方法级元数据（导出方法清单、参数签名）时，
//     可复用 Register + ServiceFuncs + CallFuncWithContext。
//
// 注意：参数以 []byte 传入（连接上流转的就是字节），由本包按方法签名
// 还原成具体类型；方法必须满足 sloth 的签名约定（见 README"服务方法签名约定"）。
package ref
