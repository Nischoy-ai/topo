package worker

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlanTargetPartitionsDeterministicAndExcluded(t *testing.T) {
	scope := TargetScope{
		ScopeID:             "site-a",
		Revision:            7,
		CIDRs:               []string{"10.0.1.9/23", "2001:db8::/63", "10.0.0.0/24"},
		Exclusions:          []string{"10.0.1.128/25", "2001:db8:0:1::/64"},
		IPv4PartitionPrefix: 25,
		IPv6PartitionPrefix: 64,
	}
	first, err := PlanTargetPartitions(scope)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanTargetPartitions(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("partition plan changed across identical revisions:\nfirst=%#v\nsecond=%#v", first, second)
	}
	body, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"key":"2e21f4d921228576ffba0d1bd4b17eef74a841f63076289d653466cfccaf9d73","ordinal":0,"count":4,"cidrs":["10.0.0.0/25"]},{"key":"bac907d0eb53da4470e1db35820ca988bf247b47978cbf3220a7b00eaccf1423","ordinal":1,"count":4,"cidrs":["10.0.0.128/25"]},{"key":"7b24620f007cebec1a242ca133b9ab070bc04a4ca911d71cb30eb403dfcc4a4b","ordinal":2,"count":4,"cidrs":["10.0.1.0/25"]},{"key":"53286305e7b3d8fba1edb9e38739ac1559b1475e7550518be29ef47f7e9e4796","ordinal":3,"count":4,"cidrs":["2001:db8::/64"]}]`
	if string(body) != want {
		t.Fatalf("canonical partitions = %s, want %s", body, want)
	}
}

func TestPlanTargetPartitionsBoundsAndCanonicalizes(t *testing.T) {
	tests := []struct {
		name  string
		scope TargetScope
	}{
		{name: "invalid scope", scope: TargetScope{ScopeID: "bad scope", Revision: 1, CIDRs: []string{"10.0.0.0/24"}}},
		{name: "invalid revision", scope: TargetScope{ScopeID: "scope", CIDRs: []string{"10.0.0.0/24"}}},
		{name: "empty", scope: TargetScope{ScopeID: "scope", Revision: 1}},
		{name: "invalid cidr", scope: TargetScope{ScopeID: "scope", Revision: 1, CIDRs: []string{"not-a-cidr"}}},
		{name: "prefix", scope: TargetScope{ScopeID: "scope", Revision: 1, CIDRs: []string{"10.0.0.0/24"}, IPv4PartitionPrefix: 7}},
		{name: "partition overflow", scope: TargetScope{ScopeID: "scope", Revision: 1, CIDRs: []string{"10.0.0.0/16"}, IPv4PartitionPrefix: 32, MaxPartitions: 10}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PlanTargetPartitions(test.scope); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	partitions, err := PlanTargetPartitions(TargetScope{
		ScopeID: "scope", Revision: 1,
		CIDRs:               []string{"192.0.2.99/24", "192.0.2.0/25"},
		Exclusions:          []string{"192.0.2.64/26"},
		IPv4PartitionPrefix: 26,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(partitions))
	for index, partition := range partitions {
		got[index] = partition.CIDRs[0]
		if partition.Ordinal != index || partition.Count != len(partitions) || len(partition.Key) != 64 {
			t.Fatalf("invalid descriptor %#v", partition)
		}
	}
	want := []string{"192.0.2.0/26", "192.0.2.128/26", "192.0.2.192/26"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partitions = %v, want %v", got, want)
	}
}
