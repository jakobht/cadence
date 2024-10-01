package main

import (
	"context"
	"fmt"
	"log"

	shardmanagerv1 "github.com/uber/cadence/.gen/proto/shardmanager/v1"
	"go.uber.org/yarpc"
	"go.uber.org/yarpc/transport/grpc"
)

func main() {
	if err := do(); err != nil {
		log.Fatal(err)
	}
}

func do() error {
	dispatcher := yarpc.NewDispatcher(yarpc.Config{
		Name: "shard-manager-test-client",
		Outbounds: yarpc.Outbounds{
			"cadence-shard-manager": {
				Stream: grpc.NewTransport().NewSingleOutbound("127.0.0.1:7836"),
			},
		},
	})

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
		fmt.Printf("sending message: %v\n", i)
		err := stream.Send(&shardmanagerv1.GetShardOwnerRequest{
			ShardKey: fmt.Sprintf("shard-%v", i),
		})
		if err != nil {
			return err
		}

		fmt.Println("waiting for response...")
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		fmt.Printf("got response: %v, %v\n", msg.ShardKey, msg.Owner)
		fmt.Printf(">>> ")
	}
	return nil
}
