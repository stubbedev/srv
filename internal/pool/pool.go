// Package pool is a typed sync.Pool: Get returns a *T with no type assertion
// at call sites, so a pool can never hand out the wrong type.
package pool

import "sync"

// Pool recycles *T values. The zero value is ready to use; Get returns a
// fresh zero T when the pool is empty.
type Pool[T any] struct{ p sync.Pool }

// Get returns a pooled *T or a new one. Its contents are whatever Put left.
func (p *Pool[T]) Get() *T {
	if v, ok := p.p.Get().(*T); ok {
		return v
	}
	return new(T)
}

// Put returns v to the pool. Callers clear references v holds first, so
// the pool does not keep them alive.
func (p *Pool[T]) Put(v *T) { p.p.Put(v) }
