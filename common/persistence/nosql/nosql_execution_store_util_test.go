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

package nosql

import (
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/checksum"
	"github.com/uber/cadence/common/log"
	"github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/persistence/nosql/nosqlplugin"
)

func TestNosqlExecutionStoreUtils(t *testing.T) {
	testCases := []struct {
		name       string
		setupStore func(*nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error)
		input      *persistence.InternalWorkflowSnapshot
		validate   func(*testing.T, *nosqlplugin.WorkflowExecutionRequest, error)
	}{
		{
			name: "PrepareCreateWorkflowExecutionRequestWithMaps - Success",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				workflowSnapshot := &persistence.InternalWorkflowSnapshot{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "test-domain-id",
						WorkflowID: "test-workflow-id",
						RunID:      "test-run-id",
					},
					VersionHistories: &persistence.DataBlob{
						Encoding: common.EncodingTypeJSON,
						Data:     []byte(`[{"Branches":[{"BranchID":"test-branch-id","BeginNodeID":1,"EndNodeID":2}]}]`),
					},
				}
				return store.prepareCreateWorkflowExecutionRequestWithMaps(workflowSnapshot)
			},
			input: &persistence.InternalWorkflowSnapshot{},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				if err == nil {
					assert.NotNil(t, req)
				}
			},
		},
		{
			name: "PrepareCreateWorkflowExecutionRequestWithMaps - Nil Checksum",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				workflowSnapshot := &persistence.InternalWorkflowSnapshot{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "test-domain-id",
						WorkflowID: "test-workflow-id",
						RunID:      "test-run-id",
					},
					VersionHistories: &persistence.DataBlob{
						Encoding: common.EncodingTypeJSON,
						Data:     []byte(`[{"Branches":[{"BranchID":"test-branch-id","BeginNodeID":1,"EndNodeID":2}]}]`),
					},
					Checksum: checksum.Checksum{Value: nil},
				}
				return store.prepareCreateWorkflowExecutionRequestWithMaps(workflowSnapshot)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req.Checksums)
			},
		},

		{
			name: "PrepareCreateWorkflowExecutionRequestWithMaps - Empty VersionHistories",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				// Testing with an empty VersionHistories (which previously caused an error)
				workflowSnapshot := &persistence.InternalWorkflowSnapshot{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "test-domain-id-2",
						WorkflowID: "test-workflow-id-2",
						RunID:      "test-run-id-2",
					},
					VersionHistories: &persistence.DataBlob{
						Encoding: common.EncodingTypeJSON,
						Data:     []byte("[]"), // Empty VersionHistories
					},
				}
				return store.prepareCreateWorkflowExecutionRequestWithMaps(workflowSnapshot)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req.VersionHistories)
				assert.Equal(t, "[]", string(req.VersionHistories.Data))
			},
		},
		{
			name: "PrepareResetWorkflowExecutionRequestWithMapsAndEventBuffer - Success",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				resetWorkflow := &persistence.InternalWorkflowSnapshot{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "reset-domain-id",
						WorkflowID: "reset-workflow-id",
						RunID:      "reset-run-id",
					},
					LastWriteVersion: 123,
					Checksum:         checksum.Checksum{Version: 1},
					VersionHistories: &persistence.DataBlob{Encoding: common.EncodingTypeJSON, Data: []byte(`[{"Branches":[{"BranchID":"reset-branch-id","BeginNodeID":1,"EndNodeID":2}]}]`)},
					ActivityInfos:    []*persistence.InternalActivityInfo{{ScheduleID: 1}},
					TimerInfos:       []*persistence.TimerInfo{{TimerID: "timerID"}},
					ChildExecutionInfos: []*persistence.InternalChildExecutionInfo{
						{InitiatedID: 1, StartedID: 2},
					},
					RequestCancelInfos: []*persistence.RequestCancelInfo{{InitiatedID: 1}},
					SignalInfos:        []*persistence.SignalInfo{{InitiatedID: 1}},
					SignalRequestedIDs: []string{"signalRequestedID"},
					Condition:          999,
				}
				return store.prepareResetWorkflowExecutionRequestWithMapsAndEventBuffer(resetWorkflow)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req)
				assert.Equal(t, nosqlplugin.WorkflowExecutionMapsWriteModeReset, req.MapsWriteMode)
				assert.Equal(t, nosqlplugin.EventBufferWriteModeClear, req.EventBufferWriteMode)
				assert.Equal(t, int64(999), *req.PreviousNextEventIDCondition)
			},
		},
		{
			name: "PrepareResetWorkflowExecutionRequestWithMapsAndEventBuffer - Malformed VersionHistories",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				resetWorkflow := &persistence.InternalWorkflowSnapshot{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "domain-id-malformed-vh",
						WorkflowID: "workflow-id-malformed-vh",
						RunID:      "run-id-malformed-vh",
					},
					LastWriteVersion: 456,
					Checksum:         checksum.Checksum{Version: 1},
					VersionHistories: &persistence.DataBlob{Encoding: common.EncodingTypeJSON, Data: []byte("{malformed}")},
				}
				return store.prepareResetWorkflowExecutionRequestWithMapsAndEventBuffer(resetWorkflow)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req)
			},
		},
		{
			name: "PrepareUpdateWorkflowExecutionRequestWithMapsAndEventBuffer - Successful Update Request Preparation",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				workflowMutation := &persistence.InternalWorkflowMutation{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{
						DomainID:   "domainID-success",
						WorkflowID: "workflowID-success",
						RunID:      "runID-success",
					},
				}
				return store.prepareUpdateWorkflowExecutionRequestWithMapsAndEventBuffer(workflowMutation)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req)
			},
		},
		{
			name: "PrepareUpdateWorkflowExecutionRequestWithMapsAndEventBuffer - Incomplete WorkflowMutation",
			setupStore: func(store *nosqlExecutionStore) (*nosqlplugin.WorkflowExecutionRequest, error) {
				workflowMutation := &persistence.InternalWorkflowMutation{
					ExecutionInfo: &persistence.InternalWorkflowExecutionInfo{ // Partially populated for the test
						DomainID: "domainID-incomplete",
					},
				}
				return store.prepareUpdateWorkflowExecutionRequestWithMapsAndEventBuffer(workflowMutation)
			},
			validate: func(t *testing.T, req *nosqlplugin.WorkflowExecutionRequest, err error) {
				assert.NoError(t, err)
				assert.NotNil(t, req)
				assert.Equal(t, "domainID-incomplete", req.DomainID) // Example assertion
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockDB := nosqlplugin.NewMockDB(mockCtrl)
			store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

			req, err := tc.setupStore(store)
			tc.validate(t, req, err)
		})
	}

}

func TestPrepareTasksForWorkflowTxn(t *testing.T) {
	testCases := []struct {
		name       string
		setupStore func(*nosqlExecutionStore) ([]*nosqlplugin.TimerTask, error)
		validate   func(*testing.T, []*nosqlplugin.TimerTask, error)
	}{{
		name: "PrepareTimerTasksForWorkflowTxn - Successful Timer Tasks Preparation",
		setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.TimerTask, error) {
			timerTasks := []persistence.Task{
				&persistence.DecisionTimeoutTask{VisibilityTimestamp: time.Now(), TaskID: 1, EventID: 2, TimeoutType: 1, ScheduleAttempt: 1},
			}
			tasks, err := store.prepareTimerTasksForWorkflowTxn("domainID", "workflowID", "runID", timerTasks)
			assert.NoError(t, err)
			assert.NotEmpty(t, tasks)
			return nil, err
		},
		validate: func(t *testing.T, tasks []*nosqlplugin.TimerTask, err error) {},
	},
		{
			name: "PrepareTimerTasksForWorkflowTxn - Unsupported Timer Task Type",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.TimerTask, error) {
				timerTasks := []persistence.Task{
					&dummyTaskType{
						VisibilityTimestamp: time.Now(),
						TaskID:              1,
					},
				}
				return store.prepareTimerTasksForWorkflowTxn("domainID-unsupported", "workflowID-unsupported", "runID-unsupported", timerTasks)
			},
			validate: func(t *testing.T, tasks []*nosqlplugin.TimerTask, err error) {
				assert.Error(t, err)
				assert.Nil(t, tasks)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockDB := nosqlplugin.NewMockDB(mockCtrl)
			store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

			tasks, err := tc.setupStore(store)
			tc.validate(t, tasks, err)
		})
	}
}

func TestPrepareReplicationTasksForWorkflowTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDB := nosqlplugin.NewMockDB(mockCtrl)
	store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

	testCases := []struct {
		name       string
		setupStore func(*nosqlExecutionStore) ([]*nosqlplugin.ReplicationTask, error)
		validate   func(*testing.T, []*nosqlplugin.ReplicationTask, error)
	}{
		{
			name: "Successful Replication Tasks Preparation",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.ReplicationTask, error) {
				replicationTasks := []persistence.Task{
					&persistence.HistoryReplicationTask{
						Version: 1,
					},
				}
				return store.prepareReplicationTasksForWorkflowTxn("domainID", "workflowID", "runID", replicationTasks)
			},
			validate: func(t *testing.T, tasks []*nosqlplugin.ReplicationTask, err error) {
				assert.NoError(t, err)
				assert.NotEmpty(t, tasks)
			},
		},
		{
			name: "Handling Unknown Replication Task Type",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.ReplicationTask, error) {
				replicationTasks := []persistence.Task{
					&dummyTaskType{
						VisibilityTimestamp: time.Now(),
						TaskID:              -1,
					},
				}
				return store.prepareReplicationTasksForWorkflowTxn("domainID", "workflowID", "runID", replicationTasks)
			},
			validate: func(t *testing.T, tasks []*nosqlplugin.ReplicationTask, err error) {
				assert.Error(t, err)
				assert.Nil(t, tasks)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := tc.setupStore(store)
			tc.validate(t, tasks, err)
		})
	}
}

func TestPrepareCrossClusterTasksForWorkflowTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDB := nosqlplugin.NewMockDB(mockCtrl)
	store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

	testCases := []struct {
		name       string
		setupStore func(*nosqlExecutionStore) ([]*nosqlplugin.CrossClusterTask, error)
		validate   func(*testing.T, []*nosqlplugin.CrossClusterTask, error)
	}{
		{
			name: "Successful CrossCluster Tasks Preparation",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.CrossClusterTask, error) {
				crossClusterTasks := []persistence.Task{
					&persistence.CrossClusterStartChildExecutionTask{
						TargetCluster: "targetCluster",
					},
				}
				return store.prepareCrossClusterTasksForWorkflowTxn("domainID", "workflowID", "runID", crossClusterTasks)
			},
			validate: func(t *testing.T, tasks []*nosqlplugin.CrossClusterTask, err error) {
				assert.NoError(t, err)
				assert.NotEmpty(t, tasks)
			},
		},
		{
			name: "Handling Unsupported CrossCluster Task Type",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.CrossClusterTask, error) {
				crossClusterTasks := []persistence.Task{
					&dummyTaskType{ // Adjust this to be an unexpected type for cross-cluster tasks
						VisibilityTimestamp: time.Now(),
						TaskID:              -1,
					},
				}
				return store.prepareCrossClusterTasksForWorkflowTxn("domainID", "workflowID", "runID", crossClusterTasks)
			},
			validate: func(t *testing.T, tasks []*nosqlplugin.CrossClusterTask, err error) {
				assert.Error(t, err) // Expecting an error due to unsupported task type
				assert.Nil(t, tasks)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tasks, err := tc.setupStore(store)
			tc.validate(t, tasks, err)
		})
	}
}

func TestPrepareNoSQLTasksForWorkflowTxn(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockDB := nosqlplugin.NewMockDB(mockCtrl)
	store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

	testCases := []struct {
		name       string
		setupStore func(*nosqlExecutionStore) ([]*nosqlplugin.TransferTask, []*nosqlplugin.CrossClusterTask, []*nosqlplugin.ReplicationTask, []*nosqlplugin.TimerTask, error)
		validate   func(*testing.T, []*nosqlplugin.TransferTask, []*nosqlplugin.CrossClusterTask, []*nosqlplugin.ReplicationTask, []*nosqlplugin.TimerTask, error)
	}{
		{
			name: "prepareNoSQLTasksForWorkflowTxn - Success",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.TransferTask, []*nosqlplugin.CrossClusterTask, []*nosqlplugin.ReplicationTask, []*nosqlplugin.TimerTask, error) {
				return nil, nil, nil, nil, nil
			},
			validate: func(t *testing.T, transferTasks []*nosqlplugin.TransferTask, crossClusterTasks []*nosqlplugin.CrossClusterTask, replicationTasks []*nosqlplugin.ReplicationTask, timerTasks []*nosqlplugin.TimerTask, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "prepareNoSQLTasksForWorkflowTxn - Task Preparation Failure",
			setupStore: func(store *nosqlExecutionStore) ([]*nosqlplugin.TransferTask, []*nosqlplugin.CrossClusterTask, []*nosqlplugin.ReplicationTask, []*nosqlplugin.TimerTask, error) {
				return nil, nil, nil, nil, errors.New("task preparation failed")
			},
			validate: func(t *testing.T, transferTasks []*nosqlplugin.TransferTask, crossClusterTasks []*nosqlplugin.CrossClusterTask, replicationTasks []*nosqlplugin.ReplicationTask, timerTasks []*nosqlplugin.TimerTask, err error) {
				assert.Error(t, err)
				assert.Nil(t, transferTasks)
				assert.Nil(t, crossClusterTasks)
				assert.Nil(t, replicationTasks)
				assert.Nil(t, timerTasks)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transferTasks, crossClusterTasks, replicationTasks, timerTasks, err := tc.setupStore(store)
			tc.validate(t, transferTasks, crossClusterTasks, replicationTasks, timerTasks, err)
		})
	}
}

func TestPrepareTransferTasksForWorkflowTxn(t *testing.T) {
	testCases := []struct {
		name       string
		tasks      []persistence.Task
		expectFunc func(*nosqlplugin.MockDB)
		validate   func(*testing.T, []*nosqlplugin.TransferTask, error)
	}{
		{
			name: "Success - Prepare Transfer Tasks",
			tasks: []persistence.Task{
				&persistence.ActivityTask{
					DomainID: "domainID",
					TaskID:   1,
				},
			},
			expectFunc: func(mockDB *nosqlplugin.MockDB) {},
			validate: func(t *testing.T, tasks []*nosqlplugin.TransferTask, err error) {
				assert.NoError(t, err)
				assert.NotEmpty(t, tasks)
			},
		},
		{
			name: "Failure - Unsupported Task Type",
			tasks: []persistence.Task{
				&dummyTaskType{
					VisibilityTimestamp: time.Now(),
					TaskID:              -1,
				},
			},
			expectFunc: func(mockDB *nosqlplugin.MockDB) {},
			validate: func(t *testing.T, tasks []*nosqlplugin.TransferTask, err error) {
				assert.Error(t, err)
				assert.Nil(t, tasks)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockDB := nosqlplugin.NewMockDB(mockCtrl)
			store := newTestNosqlExecutionStore(mockDB, log.NewNoop())

			if tc.expectFunc != nil {
				tc.expectFunc(mockDB) // Set up any expectations on the mockDB
			}

			tasks, err := store.prepareTransferTasksForWorkflowTxn("domainID", "workflowID", "runID", tc.tasks)
			tc.validate(t, tasks, err) // Validate the output
		})
	}
}

type dummyTaskType struct {
	persistence.Task
	VisibilityTimestamp time.Time
	TaskID              int64
}

func (d *dummyTaskType) GetType() int {
	return 999 // Using a type that is not expected by the switch statement
}

func (d *dummyTaskType) GetVersion() int64 {
	return 1
}

func (d *dummyTaskType) SetVersion(version int64) {}
