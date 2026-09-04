package worker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type checkControl struct {
	registerCalls   int
	heartbeatCalls  int
	claimCalls      int
	renewCalls      int
	credentialCalls int
	resultCalls     int
	completeCalls   int
	register        RegisterRequest
	heartbeat       HeartbeatRequest
	heartbeatErr    error
}

func (c *checkControl) Register(_ context.Context, request RegisterRequest) (RegisterResponse, error) {
	c.registerCalls++
	c.register = request
	return RegisterResponse{WorkerID: "worker-check"}, nil
}

func (c *checkControl) Heartbeat(_ context.Context, request HeartbeatRequest) (HeartbeatResponse, error) {
	c.heartbeatCalls++
	c.heartbeat = request
	return HeartbeatResponse{}, c.heartbeatErr
}

func (c *checkControl) Claim(context.Context, ClaimRequest) (ClaimResponse, error) {
	c.claimCalls++
	return ClaimResponse{}, errors.New("check must not claim")
}

func (c *checkControl) Renew(context.Context, string, RenewRequest) (RenewResponse, error) {
	c.renewCalls++
	return RenewResponse{}, errors.New("check must not renew")
}

func (c *checkControl) Credential(context.Context, string, CredentialRequest) (SSHCredential, error) {
	c.credentialCalls++
	return SSHCredential{}, errors.New("check must not resolve a credential")
}

func (c *checkControl) SubmitResult(context.Context, string, ResultChunkRequest) (ResultChunkResponse, error) {
	c.resultCalls++
	return ResultChunkResponse{}, errors.New("check must not submit a result")
}

func (c *checkControl) Complete(context.Context, string, CompleteRequest) (CompleteResponse, error) {
	c.completeCalls++
	return CompleteResponse{}, errors.New("check must not complete work")
}

func TestCheckRegistersAndHeartbeatsWithoutClaiming(t *testing.T) {
	control := &checkControl{}
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	policy := Policy{
		WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true,
		MaxTaskDuration: DefaultMaxTaskDuration, MaxConcurrency: 2,
	}
	result, err := Check(context.Background(), CheckConfig{
		Policy: policy, Version: "v-test", Control: control,
		Now: func() time.Time { return now }, BootID: "boot-check",
	})
	if err != nil {
		t.Fatal(err)
	}
	if control.registerCalls != 1 || control.heartbeatCalls != 1 || control.claimCalls != 0 || control.renewCalls != 0 || control.credentialCalls != 0 || control.resultCalls != 0 || control.completeCalls != 0 {
		t.Fatalf("unexpected control calls: %#v", control)
	}
	if control.register.BootID != "boot-check" || control.register.WorkerPool != "pool-a" || control.register.SiteID != "site-a" || control.register.StartedAt != now {
		t.Fatalf("register request = %#v", control.register)
	}
	if !reflect.DeepEqual(control.register.Capabilities, []string{OperationLocalV1}) {
		t.Fatalf("capabilities = %#v", control.register.Capabilities)
	}
	if control.heartbeat.WorkerID != "worker-check" || control.heartbeat.BootID != "boot-check" || control.heartbeat.CurrentLeases != 0 || control.heartbeat.SentAt != now {
		t.Fatalf("heartbeat request = %#v", control.heartbeat)
	}
	if result.Status != "ready" || result.WorkerID != "worker-check" || result.BootID != "boot-check" || result.WorkerPool != "pool-a" || result.SiteID != "site-a" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCheckBoundsHeartbeatFailure(t *testing.T) {
	control := &checkControl{heartbeatErr: errors.New(strings.Repeat("x", maxFailureBytes+100))}
	_, err := Check(context.Background(), CheckConfig{
		Policy:  Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true},
		Control: control, BootID: "boot-check",
	})
	if err == nil || len(err.Error()) > maxFailureBytes+64 || !strings.HasPrefix(err.Error(), "heartbeat ServiceNow worker: ") {
		t.Fatalf("error = %v", err)
	}
	if control.claimCalls != 0 {
		t.Fatalf("claim calls = %d", control.claimCalls)
	}
}
