package etcdclient

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"time"
)

// Client is an interface that abstracts the etcd client operations used by the shard distributor.
// This interface allows for easier testing and mocking of etcd client functionality.
// The *clientv3.Client type already implements this interface.
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

	// Close closes the etcd client connection
	Close() error
}

// Config contains the configuration for connecting to an etcd cluster.
type Config struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dialTimeout"`
	Prefix      string        `yaml:"prefix"`
}
