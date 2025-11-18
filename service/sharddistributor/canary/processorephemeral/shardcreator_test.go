package processorephemeral

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/uber/cadence/common/clock"
	"github.com/uber/cadence/service/sharddistributor/client/spectatorclient"
)

func TestShardCreator_Lifecycle(t *testing.T) {
	goleak.VerifyNone(t)

	logger := zaptest.NewLogger(t)
	timeSource := clock.NewMockedTimeSource()
	ctrl := gomock.NewController(t)

	namespace := "test-namespace"

	// Create mock spectator that returns errors
	mockSpectator := spectatorclient.NewMockSpectator(ctrl)
	mockSpectator.EXPECT().
		GetShardOwner(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx interface{}, shardKey string) (*spectatorclient.ShardOwner, error) {
			// Verify the shard key is not empty
			assert.NotEmpty(t, shardKey)
			return nil, assert.AnError // Using testify's AnError for consistency
		}).
		Times(2)

	spectators := map[string]spectatorclient.Spectator{
		namespace: mockSpectator,
	}

	params := ShardCreatorParams{
		Logger:     logger,
		TimeSource: timeSource,
		Spectators: spectators,
		// Note: CanaryClient is not needed for this test as we're testing error handling before ping
	}

	creator := NewShardCreator(params, []string{namespace})
	creator.Start()

	// Wait for the goroutine to start
	timeSource.BlockUntil(1)

	// Trigger shard creation that will fail
	timeSource.Advance(shardCreationInterval + 100*time.Millisecond)
	time.Sleep(10 * time.Millisecond) // Allow processing

	// Trigger another shard creation to ensure processing continues after error
	timeSource.Advance(shardCreationInterval + 100*time.Millisecond)
	time.Sleep(10 * time.Millisecond) // Allow processing

	creator.Stop()
}
