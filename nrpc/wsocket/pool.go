package wsocket

import (
	"reflect"
	"sync"

	"github.com/w6xian/sloth/v3/message"
)

// Reset defines Reset method for pooled object.
type Reset interface {
	Reset()
}

var reflectTypePools = &typePools{
	pools: make(map[reflect.Type]*sync.Pool),
	New: func(t reflect.Type) any {
		var argv reflect.Value

		if t.Kind() == reflect.Ptr { // reply must be ptr
			argv = reflect.New(t.Elem())
		} else {
			argv = reflect.New(t)
		}

		return argv.Interface()
	},
}

type typePools struct {
	mu    sync.RWMutex
	pools map[reflect.Type]*sync.Pool
	New   func(t reflect.Type) any
}

func (p *typePools) Init(t reflect.Type) {
	tp := &sync.Pool{}
	tp.New = func() any {
		return p.New(t)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pools[t] = tp
}

func (p *typePools) Put(t reflect.Type, x any) {
	if o, ok := x.(Reset); ok {
		o.Reset()
		p.mu.RLock()
		pool := p.pools[t]
		p.mu.RUnlock()
		pool.Put(x)
	}

}

func (p *typePools) Get(t reflect.Type) any {
	p.mu.RLock()
	pool := p.pools[t]
	p.mu.RUnlock()

	return pool.Get()
}

var callObjPool sync.Pool = sync.Pool{
	New: func() any {
		return &message.JsonCallObject{}
	},
}

func getCallObj() *message.JsonCallObject {
	req := callObjPool.Get()
	if req == nil {
		return &message.JsonCallObject{}
	}
	return req.(*message.JsonCallObject)
}

func putCallObj(req *message.JsonCallObject) {
	if req == nil {
		return
	}
	req.Header = nil
	req.Method = ""
	req.Data = nil
	req.Error = ""
	req.Args = nil
	callObjPool.Put(req)
}

var backObjPool sync.Pool = sync.Pool{
	New: func() any {
		return &message.JsonBackObject{}
	},
}

func getBackObj() *message.JsonBackObject {
	req := backObjPool.Get()
	if req == nil {
		return &message.JsonBackObject{}
	}
	return req.(*message.JsonBackObject)
}

func putBackObj(req *message.JsonBackObject) {
	if req == nil {
		return
	}
	req.Context = nil
	req.Header = nil
	req.Data = nil
	req.Error = ""
	backObjPool.Put(req)
}
