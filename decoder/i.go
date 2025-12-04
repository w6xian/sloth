package decoder

// Input data for interceptor
// (拦截器输入数据)
type IcReq interface{}

// Output data for interceptor
// (拦截器输出数据)
type IcResp interface{}

// Interceptor
// (拦截器)
type IInterceptor interface {
	Intercept(IChain) IcResp
	// The interception method of the interceptor (defined by the developer)
	// (拦截器的拦截处理方法,由开发者定义)
}

// Responsibility chain
// (责任链)
type IChain interface {
	Request() IcReq        // Get the request data in the current chain (current interceptor)-获取当前责任链中的请求数据(当前拦截器)
	GetIMessage() IMessage // Get IMessage from Chain (从Chain中获取IMessage)
	Proceed(IcReq) IcResp  // Enter and execute the next interceptor, and pass the request data to the next interceptor (进入并执行下一个拦截器，且将请求数据传递给下一个拦截器)
	ProceedWithIMessage(IMessage, IcReq) IcResp
}

// IMessage Package ziface defines an abstract interface for encapsulating a request message into a message
type IMessage interface {
	GetDataLen() uint32 // Gets the length of the message data segment(获取消息数据段长度)
	GetMsgID() uint32   // Gets the ID of the message(获取消息ID)
	GetData() []byte    // Gets the content of the message(获取消息内容)
	GetRawData() []byte // Gets the raw data of the message(获取原始数据)

	SetMsgID(uint32)   // Sets the ID of the message(设计消息ID)
	SetData([]byte)    // Sets the content of the message(设计消息内容)
	SetDataLen(uint32) // Sets the length of the message data segment(设置消息数据段长度)
}
