package membership

import "regexp"

const uuidRegex = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
var uuidRegexp = regexp.MustCompile(uuidRegex)

func TaskListOwnedByShardDistributor(taskListName string) bool {
	// this regex checks if the task list name has a UUID, if it has we
	// consider it a short lived tasklist, that will not be manage by the shard distributor
	return uuidRegexp.MatchString(taskListName)
}
