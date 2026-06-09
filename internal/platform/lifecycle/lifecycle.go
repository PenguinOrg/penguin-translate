package lifecycle

import "sync"

var (
	mu    sync.Mutex
	hooks []func()
)

func Register(fn func()) {
	if fn == nil {
		return
	}
	mu.Lock()
	hooks = append(hooks, fn)
	mu.Unlock()
}

func RunAll() {
	mu.Lock()
	list := make([]func(), len(hooks))
	copy(list, hooks)
	mu.Unlock()
	for i := len(list) - 1; i >= 0; i-- {
		if list[i] != nil {
			list[i]()
		}
	}
}
