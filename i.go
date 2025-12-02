package sloth

type RpcServer interface {
	Start(addr string) error
}
