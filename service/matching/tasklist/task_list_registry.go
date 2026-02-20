package tasklist

import (
	"sync"

	"github.com/uber/cadence/common/metrics"
)

type taskListRegistryImpl struct {
	sync.RWMutex
	taskLists     map[Identifier]Manager
	metricsClient metrics.Client
}

func NewManagerRegistry(metricsClient metrics.Client) ManagerRegistry {
	return &taskListRegistryImpl{
		taskLists:     make(map[Identifier]Manager),
		metricsClient: metricsClient,
	}
}

func (r *taskListRegistryImpl) Register(id Identifier, mgr Manager) {
	r.Lock()
	defer r.Unlock()

	// we can override the manager for the same identifier if it is already registered
	// this case should be handled by the caller
	r.taskLists[id] = mgr
	r.updateMetricsLocked()
}

func (r *taskListRegistryImpl) Unregister(mgr Manager) bool {
	id := mgr.TaskListID()
	r.Lock()
	defer r.Unlock()

	// we need to make sure we still hold the given `mgr` or we already replaced with a new one.
	currentTlMgr, ok := r.taskLists[*id]
	if ok && currentTlMgr == mgr {
		delete(r.taskLists, *id)
		r.updateMetricsLocked()
		return true
	}

	return false
}

func (r *taskListRegistryImpl) ManagersByDomainID(domainID string) []Manager {
	r.RLock()
	defer r.RUnlock()

	var res []Manager
	for tl, tlm := range r.taskLists {
		if tl.GetDomainID() == domainID {
			res = append(res, tlm)
		}
	}
	return res
}

func (r *taskListRegistryImpl) ManagersByTaskListName(name string) []Manager {
	r.RLock()
	defer r.RUnlock()

	var res []Manager
	for _, tlm := range r.taskLists {
		if tlm.TaskListID().GetName() == name {
			res = append(res, tlm)
		}
	}
	return res
}

func (r *taskListRegistryImpl) ManagerByTaskListIdentifier(id Identifier) (Manager, bool) {
	r.RLock()
	defer r.RUnlock()

	tlMgr, ok := r.taskLists[id]
	return tlMgr, ok
}

func (r *taskListRegistryImpl) AllManagers() []Manager {
	r.RLock()
	defer r.RUnlock()

	res := make([]Manager, 0, len(r.taskLists))
	for _, tlMgr := range r.taskLists {
		res = append(res, tlMgr)
	}
	return res
}

func (r *taskListRegistryImpl) updateMetricsLocked() {
	r.metricsClient.Scope(metrics.MatchingTaskListMgrScope).UpdateGauge(
		metrics.TaskListManagersGauge,
		float64(len(r.taskLists)),
	)
}
