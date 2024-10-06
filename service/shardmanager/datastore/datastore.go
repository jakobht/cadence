// The MIT License (MIT)

// Copyright (c) 2017-2020 Uber Technologies Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package datastore

import (
	"fmt"
	"math/rand"
	"sync"

	"github.com/uber/cadence/client/matching"
)

type DataStore interface {
	GetShardInfo(id ShardID) (ShardInfo, error)
	PutShardInfo(id ShardID, info ShardInfo) error
	GetShardOwner(id ShardID) (string, error)
}

type ShardID string
type ShardInfo struct {
	Owner string
	Load  float64
}

func NewDataStore(peerResolver matching.PeerResolver) DataStore {
	return &dataStore{
		data:         make(map[ShardID]ShardInfo),
		peerResolver: peerResolver,
	}
}

type dataStore struct {
	sync.Mutex
	data         map[ShardID]ShardInfo
	peerResolver matching.PeerResolver
}

func (d *dataStore) GetShardInfo(id ShardID) (ShardInfo, error) {
	d.Lock()
	defer d.Unlock()
	info, ok := d.data[id]
	if !ok {
		return ShardInfo{}, fmt.Errorf("shard not found")
	}
	return info, nil
}

func (d *dataStore) PutShardInfo(id ShardID, info ShardInfo) error {
	d.Lock()
	defer d.Unlock()
	d.data[id] = info
	return nil
}

func (d *dataStore) GetShardOwner(id ShardID) (string, error) {
	d.Lock()
	defer d.Unlock()

	info, ok := d.data[id]
	if ok {
		return info.Owner, nil
	}

	allPeers, err := d.peerResolver.GetAllPeers()
	if err != nil {
		return "", fmt.Errorf("get all peers %w", err)
	}

	// assign shard to a random peer
	if len(allPeers) == 0 {
		return "", fmt.Errorf("no peers available")
	}

	owner := allPeers[rand.Intn(len(allPeers))]

	d.data[id] = ShardInfo{
		Owner: owner,
		Load:  0,
	}

	return owner, nil
}
