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

package types

import "fmt"

//go:generate enumer -type=ExecutorStatus,ShardStatus,AssignmentStatus -json -output sharddistributor_statuses_enumer_generated.go

type GetShardOwnerRequest struct {
	ShardKey  string
	Namespace string
}

func (v *GetShardOwnerRequest) GetShardKey() (o string) {
	if v != nil {
		return v.ShardKey
	}
	return
}

func (v *GetShardOwnerRequest) GetNamespace() (o string) {
	if v != nil {
		return v.Namespace
	}
	return
}

type GetShardOwnerResponse struct {
	Owner     string
	Namespace string
}

func (v *GetShardOwnerResponse) GetOwner() (o string) {
	if v != nil {
		return v.Owner
	}
	return
}

func (v *GetShardOwnerResponse) GetNamespace() (o string) {
	if v != nil {
		return v.Namespace
	}
	return
}

type NamespaceNotFoundError struct {
	Namespace string
}

func (n *NamespaceNotFoundError) Error() (o string) {
	if n != nil {
		return fmt.Sprintf("namespace not found %v", n.Namespace)
	}
	return
}

type ShardNotFoundError struct {
	Namespace string
	ShardKey  string
}

func (n *ShardNotFoundError) Error() (o string) {
	if n != nil {
		return fmt.Sprintf("shard not found %v:%v", n.Namespace, n.ShardKey)
	}
	return
}

type NewEphemeralShardRequest struct {
	ShardKey  string
	Namespace string
}

func (v *NewEphemeralShardRequest) GetShardKey() (o string) {
	if v != nil {
		return v.ShardKey
	}
	return
}

func (v *NewEphemeralShardRequest) GetNamespace() (o string) {
	if v != nil {
		return v.Namespace
	}
	return
}

type NewEphemeralShardResponse struct {
	Owner     string
	Namespace string
}

func (v *NewEphemeralShardResponse) GetOwner() (o string) {
	if v != nil {
		return v.Owner
	}
	return
}

func (v *NewEphemeralShardResponse) GetNamespace() (o string) {
	if v != nil {
		return v.Namespace
	}
	return
}

type ExecutorHeartbeatRequest struct {
	Namespace          string
	ExecutorID         string
	Status             ExecutorStatus
	ShardStatusReports map[string]*ShardStatusReport
}

func (v *ExecutorHeartbeatRequest) GetNamespace() (o string) {
	if v != nil {
		return v.Namespace
	}
	return
}

func (v *ExecutorHeartbeatRequest) GetExecutorID() (o string) {
	if v != nil {
		return v.ExecutorID
	}
	return
}

func (v *ExecutorHeartbeatRequest) GetStatus() (o ExecutorStatus) {
	if v != nil {
		return v.Status
	}
	return
}

func (v *ExecutorHeartbeatRequest) GetShardStatusReports() (o map[string]*ShardStatusReport) {
	if v != nil {
		return v.ShardStatusReports
	}
	return
}

// ExecutorStatus is persisted to the DB with a string value mapping.
// Beware - if we want to change the name - it should be backward compatible and should be done in two steps.
type ExecutorStatus int32

const (
	ExecutorStatusINVALID  ExecutorStatus = 0
	ExecutorStatusACTIVE   ExecutorStatus = 1
	ExecutorStatusDRAINING ExecutorStatus = 2
	ExecutorStatusDRAINED  ExecutorStatus = 3
)

type ShardStatusReport struct {
	Status    ShardStatus
	ShardLoad float64
}

func (v *ShardStatusReport) GetStatus() (o ShardStatus) {
	if v != nil {
		return v.Status
	}
	return
}

func (v *ShardStatusReport) GetShardLoad() (o float64) {
	if v != nil {
		return v.ShardLoad
	}
	return
}

// ShardStatus is ppersisted to the DB with a string value mapping.
// Beware - if we want to change the name - it should be backward compatible and should be done in two steps.
type ShardStatus int32

const (
	ShardStatusINVALID ShardStatus = 0
	ShardStatusREADY   ShardStatus = 1
)

type ExecutorHeartbeatResponse struct {
	ShardAssignments map[string]*ShardAssignment
}

func (v *ExecutorHeartbeatResponse) GetShardAssignments() (o map[string]*ShardAssignment) {
	if v != nil {
		return v.ShardAssignments
	}
	return
}

type ShardAssignment struct {
	Status AssignmentStatus
}

func (v *ShardAssignment) GetStatus() (o AssignmentStatus) {
	if v != nil {
		return v.Status
	}
	return
}

// AssignmentStatus is persisted to the DB with a string value mapping.
// Beware - if we want to change the name - it should be backward compatible and should be done in two steps.
type AssignmentStatus int32

const (
	AssignmentStatusINVALID AssignmentStatus = 0
	AssignmentStatusREADY   AssignmentStatus = 1
)
