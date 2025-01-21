package membership

import "github.com/uber/cadence/common"

type SingleProvider interface {
	common.Daemon
	Lookup(key string) (HostInfo, error)
	Subscribe(name string, channel chan<- *ChangedEvent) error
	Unsubscribe(name string) error
	Members() []HostInfo
	MemberCount() int
}
