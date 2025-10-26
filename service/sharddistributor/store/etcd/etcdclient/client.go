package etcdclient

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"time"
)

// Client is an interface that abstracts the etcd client operations used by the shard distributor.
// This interface allows for easier testing and mocking of etcd client functionality.
type Client interface {
	// Get retrieves keys from etcd
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)

	// Put stores a key-value pair in etcd
	Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error)

	// Delete removes a key from etcd
	Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error)

	// Watch watches for changes to keys in etcd
	Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan

	// Txn creates a transaction for atomic operations
	Txn(ctx context.Context) clientv3.Txn

	// GetSession creates a new session for concurrency operations like elections and distributed locks
	GetSession(ctx context.Context, opts ...concurrency.SessionOption) (*concurrency.Session, error)

	// Close closes the etcd client connection
	Close() error
}

// clientImpl is a concrete implementation of the Client interface wrapping *clientv3.Client
type clientImpl struct {
	*clientv3.Client
}

// NewClient creates a new Client from etcd configuration
func NewClient(cfg Config) (Client, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &clientImpl{Client: client}, nil
}

// NewClientWrapper wraps an existing *clientv3.Client to implement the Client interface
func NewClientWrapper(client *clientv3.Client) Client {
	return &clientImpl{Client: client}
}

// GetSession creates a new session for concurrency operations
func (c *clientImpl) GetSession(ctx context.Context, opts ...concurrency.SessionOption) (*concurrency.Session, error) {
	return concurrency.NewSession(c.Client, opts...)
}

// Config contains the configuration for connecting to an etcd cluster.
type Config struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dialTimeout"`
	Prefix      string        `yaml:"prefix"`
}
