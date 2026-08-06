package tui

import "github.com/lyracorp/xmanager/internal/tui/shared"

type Router struct {
	stack []shared.ScreenID
}

func NewRouter() *Router {
	return &Router{
		stack: []shared.ScreenID{shared.ScreenFleetOverview},
	}
}

func (r *Router) Current() shared.ScreenID {
	if len(r.stack) == 0 {
		return shared.ScreenFleetOverview
	}
	return r.stack[len(r.stack)-1]
}

func (r *Router) Push(screen shared.ScreenID) {
	r.stack = append(r.stack, screen)
}

func (r *Router) Pop() shared.ScreenID {
	if len(r.stack) <= 1 {
		return r.Current()
	}
	r.stack = r.stack[:len(r.stack)-1]
	return r.Current()
}

func (r *Router) Reset(screen shared.ScreenID) {
	r.stack = []shared.ScreenID{screen}
}

func (r *Router) Replace(screen shared.ScreenID) {
	if len(r.stack) == 0 {
		r.stack = []shared.ScreenID{screen}
		return
	}
	r.stack[len(r.stack)-1] = screen
}

func (r *Router) Depth() int {
	return len(r.stack)
}
