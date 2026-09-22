// Package transport 是与传输协议无关的"帧分发层"：拿到一帧字节后，
// 判定它是 @FN 帧还是普通 Data 帧，交给对应的 handler。
//
// 谁需要直接用它：
//   - 用 sloth 收发消息：不用，传输层已经调好了；
//     nrpc.DispatchMessage 是本包的门面，nrpc.RouteMessage 就是
//     RouteHandler 的别名；
//   - 实现自定义传输（KCP、私有协议…）：收完整一帧后调
//     NewFrameRouter(onFn, onData).Dispatch(ctx, raw)，
//     就能与 ws / TCP / QUIC 走同一套分发与编解码选择逻辑。
//
// 这样"分发"与"传输"解耦：新增一种传输只需要负责收发字节，
// 不必再复制一遍路由判断。
package transport
