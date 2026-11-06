// Package lru is a least-recently-used cache bounded by the total weight of what it holds. Core: the caches of the GitHub
// transport, the package registries and the image proxy are this one. It has no lock: the owner guards it with its own.
package lru

import "container/list"

// Cache holds values by key up to a total weight (weigh of each value), the least recently used out first.
type Cache[K comparable, V any] struct {
	max, size int
	weigh     func(V) int
	order     *list.List // of *entry[K, V], the most recent at the front
	byKey     map[K]*list.Element
}

type entry[K comparable, V any] struct {
	key    K
	val    V
	weight int // as it was counted when it came in
}

// New is an empty cache of at most max in total weight (at least 1 each). A nil weigh counts each value as 1: max is then a number of values.
func New[K comparable, V any](max int, weigh func(V) int) *Cache[K, V] {
	if weigh == nil {
		weigh = func(V) int { return 1 }
	}
	return &Cache[K, V]{max: max, weigh: weigh, order: list.New(), byKey: map[K]*list.Element{}}
}

// Get is the value held for key, which becomes the most recently used.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	el, ok := c.byKey[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return of[K, V](el).val, true
}

// Put holds val for key, replacing what was there, and drops the least recently used values past the maximum weight. A value
// heavier than the maximum is not held (and leaves nothing else out). Every value weighs at least 1, so the number of values is
// bounded too, whatever weigh says of an empty one.
func (c *Cache[K, V]) Put(key K, val V) {
	c.Remove(key)
	w := max(1, c.weigh(val))
	if w > c.max {
		return
	}
	c.byKey[key] = c.order.PushFront(&entry[K, V]{key: key, val: val, weight: w})
	c.size += w
	for c.size > c.max {
		c.removeElement(c.order.Back())
	}
}

// Remove drops key.
func (c *Cache[K, V]) Remove(key K) {
	if el, ok := c.byKey[key]; ok {
		c.removeElement(el)
	}
}

// Len is the number of values held and Size their total weight.
func (c *Cache[K, V]) Len() int  { return c.order.Len() }
func (c *Cache[K, V]) Size() int { return c.size }

func (c *Cache[K, V]) removeElement(el *list.Element) {
	e := of[K, V](el)
	c.order.Remove(el)
	delete(c.byKey, e.key)
	c.size -= e.weight
}

// of is the entry of an element: the list holds nothing else.
func of[K comparable, V any](el *list.Element) *entry[K, V] {
	return el.Value.(*entry[K, V]) //nolint:forcetypeassert // the only type stored
}
