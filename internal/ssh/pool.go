package ssh

import (
	"fmt"
	"sync"
	"time"
)

type Pool struct {
	mu      sync.RWMutex
	clients map[uint]*Client
	execs   map[uint]*Executor
}

func NewPool() *Pool {
	return &Pool{
		clients: make(map[uint]*Client),
		execs:   make(map[uint]*Executor),
	}
}

func (p *Pool) Connect(serverID uint, cfg ClientConfig) (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.clients[serverID]; ok {
		if existing.IsConnected() {
			return existing, nil
		}
		existing.Close()
		delete(p.clients, serverID)
		delete(p.execs, serverID)
	}

	client := NewClient(cfg)
	if err := client.Connect(); err != nil {
		return nil, err
	}

	p.clients[serverID] = client
	p.execs[serverID] = NewExecutor(client)
	return client, nil
}

func (p *Pool) GetClient(serverID uint) (*Client, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c, ok := p.clients[serverID]
	return c, ok
}

func (p *Pool) GetExecutor(serverID uint) (*Executor, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.execs[serverID]
	return e, ok
}

func (p *Pool) Disconnect(serverID uint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[serverID]; ok {
		c.Close()
		delete(p.clients, serverID)
		delete(p.execs, serverID)
	}
}

func (p *Pool) DisconnectAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.clients {
		c.Close()
		delete(p.clients, id)
		delete(p.execs, id)
	}
}

func (p *Pool) Reconnect(serverID uint, cfg ClientConfig) (*Client, error) {
	p.Disconnect(serverID)
	return p.Connect(serverID, cfg)
}

func (p *Pool) ConnectWithRetry(serverID uint, cfg ClientConfig, maxRetries int) (*Client, error) {
	var lastErr error
	for i := 0; i <= maxRetries; i++ {
		client, err := p.Connect(serverID, cfg)
		if err == nil {
			return client, nil
		}
		lastErr = err
		if i < maxRetries {
			backoff := time.Duration(1<<uint(i)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)
		}
	}
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func (p *Pool) ActiveConnections() []uint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	ids := make([]uint, 0, len(p.clients))
	for id := range p.clients {
		ids = append(ids, id)
	}
	return ids
}
