/*
Package sloth is a rpc framework.
*/
package sloth

// 这是一个基于websocket的双向同步rpc框架，支持服务注册、调用
// 主要流程
// 服务端：
/*
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// 启一个默认的服务端
	server := sloth.DefaultServer()
	// 通过sloth.ServerConn()来创建一个服务端连接对象，传入服务端对象和一些选项
	drpc := sloth.ServerConn(server)
	r := mux.NewRouter()
	// Register services
	// drpc.Register()来注册服务，传入服务名、服务对象和元数据,HelloService中符合条件的方法会自己注册，供调用
	drpc.Register("v1", &HelloService{}, "metadata")

	drpc.Listen(ctx, "ws", "localhost:8990",
		option.WithRouter(r, "/ws"),
		option.WithOrigin("*", "localhost:8000"),
		option.WithServerHandleMessage(&Handler{})) // 注入插槽，OnConnect,OnReady,OnClose,OnData,OnError
	// 可以接受其它的client端注册上来，成为RPC服务端一个服务节点。增加服务的服务能力。
	drpc.UseProxyHandler(func(ctx context.Context, service string) (int64, error) {
		node, err := sloth.GetNode(service)
		if err != nil {
			return 0, err
		}

		svrId, ok := smap.Get(node.Service)
		if !ok {
			return 0, fmt.Errorf("service %s not registered", node.Service)
		}
		return svrId, nil
	})
	// 启动服务
	if err := drpc.Serve(); err != nil {
		panic(err)
	}
*/
/* 服务端调用客户注册的服务
rst, err := server.CallRoom(ctx, 1, "shop.Test", nil, []byte{1}, 655360, true, &AB{A: 1, B: 2}, 'a', 12345)
if err != nil {
	fmt.Println("Call error:", err)
	continue
}

*/
// 客户端：
/*
	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	newConnect.Register("shop", &HelloService{}, "")
	// Get service methods
	go newConnect.Dial(ctx, "ws", "localhost:8990")
	// 调用服务器声明的v1中的Sign方法,根据Sign方法需要传入参数
	client.Call(context.Background(), "v1.Sign", []byte("sign"))

*/
