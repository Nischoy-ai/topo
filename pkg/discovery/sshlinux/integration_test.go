package sshlinux_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/sshlinux"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
	"golang.org/x/crypto/ssh"
)

func TestDiscoverFiveHundredHostsThroughRealSSHSessions(t *testing.T) {
	estate := makeEstate(t, lab.DefaultScenario(500, 0, 42))
	server := makeServer(t, estate)
	plugin := pluginForServer(server)
	targets := make([]string, 0, len(estate.Hosts))
	for _, host := range estate.Hosts {
		targets = append(targets, host.ID+"@lab.test:22")
	}
	request := discovery.Request{SiteID: estate.Scenario.SiteID, CollectorID: "ssh-test", Targets: targets}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCompleteEstate(t, estate, first)

	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}
	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertCompleteEstate(t, estate, second)
	if err := repository.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}
	assets, err := repository.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1000 {
		t.Fatalf("repeated SSH scan created duplicates: got %d assets, want 1000", len(assets))
	}
}

func TestSSHPermissionFailureReturnsPartialInventory(t *testing.T) {
	scenario := lab.DefaultScenario(1, 0, 7)
	scenario.Faults.PermissionDeniedPercent = 100
	estate := makeEstate(t, scenario)
	observation := discoverOne(t, estate, lab.LabSSHPassword)
	if len(observation.Assets) != 2 || len(observation.Errors) != 2 {
		t.Fatalf("got assets=%d errors=%d, want partial host with two errors", len(observation.Assets), len(observation.Errors))
	}
	for _, collectionError := range observation.Errors {
		if collectionError.Code != "ssh_partial" || collectionError.Retryable {
			t.Fatalf("unexpected partial error: %#v", collectionError)
		}
	}
}

func TestSSHMalformedAndAuthenticationFailuresAreIsolated(t *testing.T) {
	for _, test := range []struct {
		name     string
		fault    lab.Faults
		password string
		code     string
	}{
		{name: "malformed", fault: lab.Faults{MalformedPercent: 100}, password: lab.LabSSHPassword, code: "ssh_parse"},
		{name: "authentication", password: "wrong", code: "ssh_connect"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scenario := lab.DefaultScenario(1, 0, 8)
			scenario.Faults = test.fault
			estate := makeEstate(t, scenario)
			observation := discoverOne(t, estate, test.password)
			if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != test.code {
				t.Fatalf("unexpected observation: %#v", observation)
			}
		})
	}
}

func TestLabSSHServerRejectsArbitraryCommands(t *testing.T) {
	estate := makeEstate(t, lab.DefaultScenario(1, 0, 9))
	server := makeServer(t, estate)
	clientSide, serverSide := bufferedPipe()
	go func() { _ = server.ServeConn(serverSide) }()
	config := &ssh.ClientConfig{User: estate.Hosts[0].ID, Auth: []ssh.AuthMethod{ssh.Password(lab.LabSSHPassword)}, HostKeyCallback: ssh.FixedHostKey(server.PublicKey()), Timeout: time.Second}
	connection, channels, requests, err := ssh.NewClientConn(clientSide, "lab.test:22", config)
	if err != nil {
		t.Fatal(err)
	}
	client := ssh.NewClient(connection, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.CombinedOutput("whoami")
	if err == nil || !strings.Contains(string(output), "allowlist") {
		t.Fatalf("arbitrary command was not rejected: output=%q err=%v", output, err)
	}
}

func pluginForServer(server *lab.SSHServer) sshlinux.Plugin {
	return sshlinux.Plugin{Config: sshlinux.Config{
		Password:        lab.LabSSHPassword,
		HostKeyCallback: ssh.FixedHostKey(server.PublicKey()),
		Concurrency:     64,
		ConnectTimeout:  2 * time.Second,
		CommandTimeout:  2 * time.Second,
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			clientSide, serverSide := bufferedPipe()
			go func() { _ = server.ServeConn(serverSide) }()
			return clientSide, nil
		},
	}}
}

func discoverOne(t *testing.T, estate lab.Estate, password string) model.ObservationEnvelope {
	t.Helper()
	server := makeServer(t, estate)
	plugin := pluginForServer(server)
	plugin.Config.Password = password
	observation, err := plugin.Discover(context.Background(), discovery.Request{SiteID: "lab", Targets: []string{fmt.Sprintf("%s@lab.test:22", estate.Hosts[0].ID)}})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func makeEstate(t *testing.T, scenario lab.Scenario) lab.Estate {
	t.Helper()
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	return estate
}

func makeServer(t *testing.T, estate lab.Estate) *lab.SSHServer {
	t.Helper()
	server, err := lab.NewSSHServer(estate)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func assertCompleteEstate(t *testing.T, estate lab.Estate, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("SSH discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
	evaluation := lab.Evaluate(estate.Expected, observation)
	if evaluation.CoveragePercent != 100 || evaluation.MissingAssets != 0 || evaluation.UnexpectedAssets != 0 {
		t.Fatalf("unexpected evaluation: %#v", evaluation)
	}
}

// bufferedPipe is an in-memory full-duplex transport. Unlike net.Pipe, writes
// are buffered, allowing both SSH peers to send their version line at once.
func bufferedPipe() (net.Conn, net.Conn) {
	aToB := make(chan []byte, 1024)
	bToA := make(chan []byte, 1024)
	aDone := make(chan struct{})
	bDone := make(chan struct{})
	a := &memoryConn{incoming: bToA, outgoing: aToB, selfDone: aDone, peerDone: bDone}
	b := &memoryConn{incoming: aToB, outgoing: bToA, selfDone: bDone, peerDone: aDone}
	return a, b
}

type memoryConn struct {
	incoming <-chan []byte
	outgoing chan<- []byte
	selfDone chan struct{}
	peerDone <-chan struct{}
	once     sync.Once
	buffer   bytes.Buffer
}

func (c *memoryConn) Read(p []byte) (int, error) {
	if c.buffer.Len() > 0 {
		return c.buffer.Read(p)
	}
	select {
	case data := <-c.incoming:
		_, _ = c.buffer.Write(data)
		return c.buffer.Read(p)
	case <-c.selfDone:
		return 0, net.ErrClosed
	case <-c.peerDone:
		return 0, io.EOF
	}
}

func (c *memoryConn) Write(p []byte) (int, error) {
	data := append([]byte(nil), p...)
	select {
	case c.outgoing <- data:
		return len(p), nil
	case <-c.selfDone:
		return 0, net.ErrClosed
	case <-c.peerDone:
		return 0, io.ErrClosedPipe
	}
}

func (c *memoryConn) Close() error {
	c.once.Do(func() { close(c.selfDone) })
	return nil
}

func (c *memoryConn) LocalAddr() net.Addr              { return memoryAddr("memory") }
func (c *memoryConn) RemoteAddr() net.Addr             { return memoryAddr("memory") }
func (c *memoryConn) SetDeadline(time.Time) error      { return nil }
func (c *memoryConn) SetReadDeadline(time.Time) error  { return nil }
func (c *memoryConn) SetWriteDeadline(time.Time) error { return nil }

type memoryAddr string

func (a memoryAddr) Network() string { return string(a) }
func (a memoryAddr) String() string  { return string(a) }
