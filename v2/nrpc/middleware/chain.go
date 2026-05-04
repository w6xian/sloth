package middleware

// Chain 将服务端中间件列表按注册顺序包装到最终处理函数上。
func Chain(middlewares []Middleware, final HandlerFunc) HandlerFunc {
	h := final
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
