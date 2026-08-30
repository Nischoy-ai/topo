package worker

import (
	"testing"
	"time"
)

func TestPolicyRequiresExplicitLocalAuthority(t *testing.T) {
	t.Parallel()
	valid := Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := valid.Capabilities(); len(got) != 1 || got[0] != OperationLocalV1 {
		t.Fatalf("capabilities = %#v", got)
	}
	for _, policy := range []Policy{
		{WorkerPool: "pool-a", SiteID: "site-a"},
		{WorkerPool: "bad/pool", SiteID: "site-a", AllowLocal: true},
		{WorkerPool: "pool-a", SiteID: "", AllowLocal: true},
		{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxTaskDuration: 11 * time.Minute},
		{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: MaxWorkerConcurrency + 1},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("policy %#v was accepted", policy)
		}
	}
}

func TestPolicyDigestIsStableAndPolicySensitive(t *testing.T) {
	t.Parallel()
	policy := Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}
	one, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if one != two || len(one) != 64 {
		t.Fatalf("digests = %q, %q", one, two)
	}
	changed, err := (Policy{WorkerPool: "pool-a", SiteID: "site-b", AllowLocal: true}).Digest()
	if err != nil {
		t.Fatal(err)
	}
	if changed == one {
		t.Fatal("policy digest did not change with site")
	}
	changed, err = (Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 2}).Digest()
	if err != nil {
		t.Fatal(err)
	}
	if changed == one {
		t.Fatal("policy digest did not change with local concurrency")
	}
}
