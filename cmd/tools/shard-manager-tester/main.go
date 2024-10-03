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

package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/yarpc"
	"go.uber.org/yarpc/transport/grpc"

	shardmanagerv1 "github.com/uber/cadence/.gen/proto/shardmanager/v1"
)

func main() {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go do(i, &wg)
	}
	wg.Wait()
}

func do(id int, group *sync.WaitGroup) error {
	defer group.Done()
	dispatcher := yarpc.NewDispatcher(yarpc.Config{
		Name: "shard-manager-test-client",
		Outbounds: yarpc.Outbounds{
			"cadence-shard-manager": {
				Stream: grpc.NewTransport().NewSingleOutbound("127.0.0.1:7836"),
			},
		},
	})

	timeToSleep := 100 * time.Duration(rand.Int()%1000) * time.Millisecond

	fmt.Printf("%v, time to sleep: %v\n", id, timeToSleep)

	client := shardmanagerv1.NewShardManagerAPIYARPCClient(dispatcher.MustOutboundConfig("cadence-shard-manager"))

	if err := dispatcher.Start(); err != nil {
		return fmt.Errorf("failed to start Dispatcher: %v", err)
	}
	defer dispatcher.Stop()

	// Specifying a deadline on the context affects the entire stream. As this is
	// generally not the desired behavior, we use a cancelable context instead.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.GetShardOwner(ctx, yarpc.WithHeader("test", "testtest"))
	if err != nil {
		return fmt.Errorf("failed to create stream: %s", err.Error())
	}

	for i := 0; i < 10; i++ {
		time.Sleep(timeToSleep)
		fmt.Printf("%v, sending message: %v\n", id, i)
		err := stream.Send(&shardmanagerv1.GetShardOwnerRequest{
			ShardKey: fmt.Sprintf("shard-%v-%v", id, i),
		})
		if err != nil {
			return err
		}

		fmt.Printf("%v, waiting for response...\n", id)
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		fmt.Printf("%v, got response: %v, %v\n", id, msg.ShardKey, msg.Owner)
	}
	return nil
}
