package compression

import (
	"sync"
)

type blockCache struct {
	data  map[int][]byte
	order []int
	max   int
	sync.Mutex
}

func newBlockCache(maxBlocks int) *blockCache {
	return &blockCache{
		order: make([]int, 0, maxBlocks),
		data:  make(map[int][]byte),
		max:   maxBlocks,
	}
}

func (c *blockCache) get(idx int) ([]byte, bool) {
	c.Lock()
	defer c.Unlock()
	b, ok := c.data[idx]
	if !ok {
		return nil, false
	}
	// move to end (most recently used)
	for i, k := range c.order {
		if k == idx {
			c.order = append(append(c.order[:i], c.order[i+1:]...), idx)
			break
		}
	}
	return b, true
}

func (c *blockCache) put(idx int, b []byte) {
	c.Lock()
	defer c.Unlock()
	for len(c.data) >= c.max && len(c.order) > 0 {
		evict := c.order[0]
		c.order = c.order[1:]
		delete(c.data, evict)
	}
	c.data[idx] = b
	c.order = append(c.order, idx)
}
