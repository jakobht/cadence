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
	"sort"
	"sync"
	"time"

	"github.com/uber/cadence/client/matching"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/log/tag"
)

type dataMap map[ShardID]*ShardInfo

func (d dataMap) String() string {
	var s string
	for k, v := range d {
		s += fmt.Sprintf("%v: %v %v\n", k, v.Owner, v.Load)
	}
	return s
}

type DataStore interface {
	common.Daemon

	// GetShardInfo(id ShardID) (ShardInfo, error)
	PutShardInfo(id ShardID, info ShardInfo) error
	GetShardOwner(id ShardID) (string, error)
}

type ShardID string
type ShardInfo struct {
	Owner string
	Load  float64
}

func NewDataStore(peerResolver matching.PeerResolver, logger log.Logger) DataStore {
	return &dataStore{
		data:         make(dataMap),
		peerResolver: peerResolver,
		logger:       logger,
		stopC:        make(chan struct{}),
	}
}

type dataStore struct {
	sync.Mutex
	data         dataMap
	peerResolver matching.PeerResolver
	logger       log.Logger
	stopC        chan struct{}
}

func (d *dataStore) Start() {
	go d.LoadBalanceDaemon()
}

func (d *dataStore) Stop() {
	close(d.stopC)
}

func (d *dataStore) LoadBalanceDaemon() {
	ticker := time.NewTicker(5 * time.Minute)

	for {
		select {
		case <-ticker.C:
			d.balanceLoad()
		case <-d.stopC:
			return
		}
	}
}

func (d *dataStore) balanceLoad() {
	d.Lock()
	defer d.Unlock()

	d.logger.Info("balancing load", tag.Value(d.data))

	// get all peers
	allPeers, err := d.peerResolver.GetAllPeers()
	if err != nil {
		d.logger.Error("get all peers", tag.Error(err))
		return
	}
	if len(allPeers) == 0 {
		d.logger.Warn("no peers available")
		return
	}

	// get shards sorted by load
	var shards []ShardID
	for id := range d.data {
		shards = append(shards, id)
	}

	// sort decending by load
	sort.Slice(shards, func(i, j int) bool {
		return d.data[shards[i]].Load > d.data[shards[j]].Load
	})

	type pvl struct {
		peer string
		load float64
	}

	var owners []pvl
	for _, peer := range allPeers {
		owners = append(owners, pvl{peer: peer, load: 0})
	}

	newData := make(map[ShardID]*ShardInfo)

	// assign shards to peers
	for _, id := range shards {
		info := d.data[id]

		// find peer with lowest load
		sort.Slice(owners, func(i, j int) bool {
			return owners[i].load < owners[j].load
		})

		// assign shard to peer
		owners[0].load += info.Load

		newData[id] = &ShardInfo{Owner: owners[0].peer, Load: 0}
	}

	d.data = newData

	d.logger.Info("load balanced", tag.Value(d.data), tag.Value(owners))
}

/*
func (d *dataStore) GetShardInfo(id ShardID) (ShardInfo, error) {
	d.Lock()
	defer d.Unlock()
	info, ok := d.data[id]
	if !ok {
		return ShardInfo{}, fmt.Errorf("shard not found")
	}
	return info, nil
}
*/

func (d *dataStore) PutShardInfo(id ShardID, info ShardInfo) error {
	d.Lock()
	defer d.Unlock()
	d.data[id] = &info
	return nil
}

func (d *dataStore) GetShardOwner(id ShardID) (string, error) {
	d.Lock()
	defer d.Unlock()

	info, ok := d.data[id]
	if ok {
		info.Load++
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

	d.data[id] = &ShardInfo{
		Owner: owner,
		Load:  0,
	}

	return owner, nil
}
