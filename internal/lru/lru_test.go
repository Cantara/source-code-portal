package lru

import "testing"

func TestCountBound(t *testing.T) {
	c := New[string, int](2, nil)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a") // b is now the least recently used
	c.Put("c", 3)
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been dropped")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Errorf("a = %d, %v", v, ok)
	}
	if c.Len() != 2 {
		t.Errorf("Len = %d, want 2", c.Len())
	}
}

func TestWeightBound(t *testing.T) {
	c := New[string, string](10, func(s string) int { return len(s) })
	c.Put("a", "1234")
	c.Put("b", "123456")
	if c.Size() != 10 || c.Len() != 2 {
		t.Fatalf("size %d, len %d", c.Size(), c.Len())
	}
	c.Put("a", "12") // replaced: lighter
	if c.Size() != 8 {
		t.Errorf("size after replace = %d, want 8", c.Size())
	}
	c.Put("huge", "12345678901") // heavier than the maximum: not held, nothing else out
	if _, ok := c.Get("huge"); ok || c.Len() != 2 {
		t.Errorf("an over-weight value was held or pushed another out (len %d)", c.Len())
	}
	c.Put("c", "12345") // 8 + 5 > 10: the least recently used ("b", after "a" was put) goes
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been dropped")
	}
	c.Remove("a")
	if c.Size() != 5 {
		t.Errorf("size after remove = %d, want 5", c.Size())
	}
}
