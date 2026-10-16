package workersetup

import (
	"errors"
	"os/user"
	"testing"
)

func TestServiceIdentityUsesUnitsNamedGroupNotAccountsPrimaryGroup(t *testing.T) {
	uid, gid, err := serviceIdentity(func(name string) (*user.User, error) {
		if name != "topo-worker" {
			t.Fatal("wrong unit account lookup")
		}
		return &user.User{Username: name, Uid: "12001", Gid: "19999"}, nil
	}, func(name string) (*user.Group, error) {
		if name != "topo-worker" {
			t.Fatal("wrong unit group lookup")
		}
		return &user.Group{Name: name, Gid: "12002"}, nil
	})
	if err != nil || uid != 12001 || gid != 12002 {
		t.Fatalf("used unrelated primary group: uid=%d gid=%d err=%v", uid, gid, err)
	}
}
func TestServiceIdentityFailsClosedOnMissingOrUnsafeNamedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, uid, gid, username, groupname string
		userErr, groupErr                   bool
	}{
		{name: "missing account", userErr: true},
		{name: "missing group", uid: "12001", groupErr: true},
		{name: "root user", uid: "0", gid: "12002"},
		{name: "root group", uid: "12001", gid: "0"},
		{name: "negative group", uid: "12001", gid: "-2"},
		{name: "invalid group", uid: "12001", gid: "invalid"},
		{name: "OS sentinel group", uid: "12001", gid: "4294967295"},
		{name: "overflow group", uid: "12001", gid: "4294967296"},
		{name: "inconsistent account", uid: "12001", gid: "12002", username: "other-user"},
		{name: "inconsistent group", uid: "12001", gid: "12002", groupname: "other-group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uid, gid, err := serviceIdentity(func(name string) (*user.User, error) {
				if tc.userErr {
					return nil, errors.New("lookup failed")
				}
				if tc.username != "" {
					name = tc.username
				}
				return &user.User{Username: name, Uid: tc.uid, Gid: "19999"}, nil
			}, func(name string) (*user.Group, error) {
				if tc.groupErr {
					return nil, errors.New("lookup failed")
				}
				if tc.groupname != "" {
					name = tc.groupname
				}
				return &user.Group{Name: name, Gid: tc.gid}, nil
			})
			if err == nil || uid != 0 || gid != 0 {
				t.Fatal("unsafe or missing identity accepted")
			}
		})
	}
}
