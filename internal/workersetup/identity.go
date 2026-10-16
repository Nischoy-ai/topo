package workersetup

import (
	"errors"
	"os/user"
	"strconv"
)

// The pinned unit sets both User=topo-worker and Group=topo-worker. Its
// explicit group overrides the account's primary group; never use that primary
// GID to grant access to copied credentials or to execute the preflight.
func serviceIdentity(lookupUser func(string) (*user.User, error), lookupGroup func(string) (*user.Group, error)) (int, int, error) {
	u, err := lookupUser("topo-worker")
	if err != nil || u == nil || u.Username != "topo-worker" {
		return 0, 0, errors.New("packaged topo-worker service account is missing or inconsistent")
	}
	g, err := lookupGroup("topo-worker")
	if err != nil || g == nil || g.Name != "topo-worker" {
		return 0, 0, errors.New("packaged topo-worker named service group is missing or inconsistent")
	}
	uid, err := serviceID(u.Uid)
	if err != nil {
		return 0, 0, errors.New("invalid packaged service account ID")
	}
	gid, err := serviceID(g.Gid)
	if err != nil {
		return 0, 0, errors.New("invalid packaged named service group ID")
	}
	return uid, gid, nil
}
func serviceID(value string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 || uint64(id) >= uint64(1<<32-1) {
		return 0, errors.New("service identity must be a positive non-root OS identity")
	}
	return id, nil
}
