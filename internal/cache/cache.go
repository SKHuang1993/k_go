package cache

import (
	"fmt"
	"sync"
	"time"
)

type Item struct {
	Object     interface{}
	Expiration int64
}

func (item Item) Expired() bool {

	if item.Expiration == 0 {
		return false
	}
	return time.Now().UnixNano() > item.Expiration
}

const (
	NoExpiration      time.Duration = -1
	DefaultExpiration time.Duration = 0
)

type Cache struct {
	*cache
}

type cache struct {
	mu                sync.RWMutex
	items             map[string]Item
	defaultExpiration time.Duration
	janitor           *janitor
	onEvicted         func(string interface{})
}

func NewCache() *Cache {

	return &Cache{}
}

func (c *cache) Set(k string, v interface{}, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}

	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}

	c.mu.Lock()

	c.items[k] = Item{
		Object:     v,
		Expiration: e,
	}
	c.mu.Unlock()

}

func (c *cache) set(k string, v interface{}, d time.Duration) {
	var e int64
	if d == DefaultExpiration {
		d = c.defaultExpiration
	}

	//若有传时间，则以传入的时间+当前的时间，来计算时间间隔
	if d > 0 {
		e = time.Now().Add(d).UnixNano()
	}

	c.items[k] = Item{
		Object:     v,
		Expiration: e,
	}

}

func (c *cache) setDefault(k string, v interface{}) {

	c.Set(k, v, DefaultExpiration)
}

///add the key to cache if an item doesnot not container
func (c *cache) Add(k string, v interface{}, d time.Duration) error {

	c.mu.Lock()
	_, found := c.items[k]
	if found {

		c.mu.Unlock()
		return fmt.Errorf("Item %s alreadt exists", k)

	}

	c.set(k, v, d)

	c.mu.Unlock()
	return nil

}

// item hasn't expired. Returns an error otherwise.
func (c *cache) Replace(k string, v interface{}, d time.Duration) error {

	c.mu.Lock()
	_, found := c.items[k]

	if !found {

		c.mu.Unlock()

		return fmt.Errorf("Item %s not exists", k)

	}

	c.set(k, v, d)

	c.mu.Unlock()
	return nil

}

func (c *cache) Get(k string) (interface{}, bool) {

	c.mu.RLock()

	item, found := c.items[k]
	if !found {
		c.mu.Unlock()
		return nil, false
	}

	//还要判断是否过期
	if item.Expiration > 0 {

		if time.Now().UnixNano() > item.Expiration {
			c.mu.RUnlock()
			return nil, false
		}

	}

	c.mu.RUnlock()
	return item.Object, true

}

type janitor struct {
	stop chan bool
}
