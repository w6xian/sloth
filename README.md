# Sloth

Sloth is a high-performance, lightweight RPC framework written in Go. It is designed to provide seamless real-time communication capabilities using WebSocket and TCP, making it ideal for modern distributed systems and real-time applications.

## Background

In the era of real-time web applications, the need for efficient, bidirectional communication is paramount. Sloth bridges the gap between traditional RPC (Remote Procedure Call) and modern real-time messaging by offering a unified interface for both. Whether you are building a chat application, a live dashboard, or a microservices architecture, Sloth simplifies the complexity of network communication.

## Features

- **Dual Protocol Support**: Seamlessly switch between WebSocket and TCP transports.
- **Real-Time RPC**: Call remote methods with low latency.
- **Message Transmission**: Efficient message broadcasting and point-to-point messaging.
- **Service Registration**: Easy-to-use API for registering and discovering services.
- **Extensible**: Middleware and interceptor support for authentication, logging, and more.
- **Cross-Platform**: Works with any client that supports standard WebSocket or TCP connections.

## Usage

### Installation

```bash
go get github.com/w6xian/sloth
```

### Server Example

Create a WebSocket RPC server:

```go
package main

import (
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/nrpc/wsocket"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:8990")
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()
	
	server := sloth.DefaultServer()
	newConnect := sloth.ServerConn(server)
	newConnect.Register("v1", &HelloService{}, "")
	
	newConnect.ListenOption(
		wsocket.WithRouter(r),
		wsocket.WithServerHandle(&Handler{}),
	)
	
	http.Handle("/", r)
	http.Serve(ln, nil)
}
```

### Client Example

Connect to the server using WebSocket:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/w6xian/sloth"
	"github.com/w6xian/sloth/nrpc/wsocket"
)

func main() {
	client := sloth.DefaultClient()
	newConnect := sloth.ClientConn(client)
	
	go newConnect.StartWebsocketClient(
		wsocket.WithClientHandle(&Handler{}),
		wsocket.WithClientUriPath("/ws"),
		wsocket.WithClientServerUri("localhost:8990"),
	)
    
    // Make an RPC call
    time.Sleep(time.Second)
    resp, err := client.Call(context.Background(), "v1.Test", []byte("hello"))
    if err != nil {
        fmt.Println("Error:", err)
    } else {
        fmt.Println("Response:", string(resp))
    }
}
```

## Code Quality

The project maintains high code quality standards through:
- **Unit & Integration Tests**: Comprehensive testing coverage ensuring reliability.
- **Clean Architecture**: Modular design separating concerns between transport, protocol, and logic.
- **Performance Optimization**: Efficient handling of concurrent connections and message serialization.

## Contributors

- [w6xian](https://github.com/w6xian)

## Repository

[https://github.com/w6xian/sloth](https://github.com/w6xian/sloth)

## License

This project is licensed under the MIT License. See the LICENSE file for details.
