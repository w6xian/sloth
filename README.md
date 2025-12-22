### Websocket RPC


### Server

```
	server := sloth.DefaultServer()
	s := sloth.ServerConn(server)
	s.Register("v1", &HelloService{}, "")
	go s.Listen("tcp", "localhost:8990")
    // 等待服务端连接
    // 查看全部实便，Sign方法中的注册后得到对应的client.id
    server.Call(context.Background(),"v1.Hello",client.id, []byte("world"))
```

### Client
```
	client := sloth.DefaultCleint()
	newConnect := sloth.ClientConn(client)
	newConnect.Register("shop", &HelloService{}, "")
	go newConnect.Dial("tcp", "localhost:8990")
    // 调用服务端服务
    resp, err := client.Call(context.Background(),"v1.Hello", []byte("world"))
```
