package asynqx

import "sync"

type activityCounter struct {
	mu     sync.Mutex
	count  int
	zeroCh chan struct{}
}

func newActivityCounter() activityCounter {
	zeroCh := make(chan struct{})
	close(zeroCh)

	return activityCounter{zeroCh: zeroCh}
}

func (c *activityCounter) Add() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.count == 0 {
		c.zeroCh = make(chan struct{})
	}

	c.count++
}

func (c *activityCounter) Done() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.count <= 0 {
		panic("asynqx: negative activity counter")
	}

	c.count--
	if c.count == 0 {
		close(c.zeroCh)
	}
}

func (c *activityCounter) Wait() {
	c.mu.Lock()
	zeroCh := c.zeroCh
	c.mu.Unlock()

	<-zeroCh
}
