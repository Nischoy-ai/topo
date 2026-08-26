package mid_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/mid"
	"github.com/Nischoy-ai/topo/internal/mid/eccsim"
)

const (
	testUsername = "topo.mid"
	testPassword = "fixture-password"
)

func TestNativeSOAPRoundTrip(t *testing.T) {
	agent, err := mid.AgentName("topo-pilot")
	if err != nil {
		t.Fatal(err)
	}
	fixture := eccsim.New(testUsername, testPassword)
	want := fixture.AddOutput(
		agent,
		mid.TopicHeartbeat,
		"heartbeat",
		"mid.server",
		`name=heartbeat`,
		`<parameters><parameter name="name" value="heartbeat"/></parameters>`,
		"corr-1",
	)
	fixture.AddOutput("mid.server.someone-else", mid.TopicHeartbeat, "heartbeat", "", "", "", "")
	server := httptest.NewTLSServer(fixture)
	defer server.Close()

	client, err := mid.NewClient(server.URL, testUsername, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	records, err := client.Poll(context.Background(), agent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].SysID != want.SysID {
		t.Fatalf("records = %#v", records)
	}
	claimed, err := client.Claim(context.Background(), records[0])
	if err != nil {
		t.Fatal(err)
	}
	if claimed.State != mid.StateProcessing {
		t.Fatalf("claimed state = %q", claimed.State)
	}
	result, err := mid.Dispatch(claimed, "v0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	responseID, err := client.InsertResult(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	responses, err := client.FindResponses(context.Background(), claimed.SysID, agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 || responses[0].SysID != responseID {
		t.Fatalf("responses = %#v", responses)
	}
	response := responses[0]
	if response.Queue != mid.QueueInput || response.State != mid.StateReady || response.ResponseTo != claimed.SysID || response.Agent != agent || response.Topic != mid.TopicHeartbeat {
		t.Fatalf("response correlation = %#v", response)
	}
	if response.Parameters != claimed.Parameters || response.AgentCorrelator != claimed.AgentCorrelator {
		t.Fatalf("response parameter correlation = %#v", response)
	}
	if !strings.Contains(response.Payload, `status="success"`) {
		t.Fatalf("response payload = %q", response.Payload)
	}
	if err := client.MarkProcessed(context.Background(), claimed); err != nil {
		t.Fatal(err)
	}
	record, err := client.Get(context.Background(), claimed.SysID, agent)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != mid.StateProcessed {
		t.Fatalf("output state = %q", record.State)
	}
	if got := strings.Join(fixture.Operations(), ","); got != "getRecords,update,getRecords,insert,getRecords,update,getRecords" {
		t.Fatalf("SOAP operations = %q", got)
	}
}

func TestRunCompletesHeartbeatAndDeniesCommandAgainstSOAPSimulator(t *testing.T) {
	agent, err := mid.AgentName("topo-run")
	if err != nil {
		t.Fatal(err)
	}
	fixture := eccsim.New(testUsername, testPassword)
	heartbeat := fixture.AddOutput(agent, mid.TopicHeartbeat, "heartbeat", "mid.server", "name=heartbeat", "<parameters/>", "corr-heartbeat")
	command := fixture.AddOutput(agent, "Command", "touch /tmp/never", "192.0.2.1", "command=touch", "<script>touch /tmp/never</script>", "corr-command")
	server := httptest.NewTLSServer(fixture)
	defer server.Close()
	client, err := mid.NewClient(server.URL, testUsername, testPassword, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	state, err := mid.OpenState(t.TempDir(), "topo-run")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- mid.Run(ctx, mid.RunConfig{
			MIDName:      "topo-run",
			Version:      "v0.0.0-test",
			PollInterval: time.Hour,
			BatchSize:    2,
			Transport:    client,
			State:        state,
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		records := fixture.Records()
		processed := 0
		responses := 0
		for _, record := range records {
			if record.Queue == mid.QueueOutput && record.State == mid.StateProcessed {
				processed++
			}
			if record.Queue == mid.QueueInput {
				responses++
			}
		}
		if processed == 2 && responses == 2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("timed out waiting for simulated ECC completion: %#v", records)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	records := fixture.Records()
	var heartbeatResponse, commandResponse *mid.Record
	for index := range records {
		record := &records[index]
		if record.Queue != mid.QueueInput {
			continue
		}
		switch record.ResponseTo {
		case heartbeat.SysID:
			heartbeatResponse = record
		case command.SysID:
			commandResponse = record
		}
	}
	if heartbeatResponse == nil || heartbeatResponse.ErrorString != "" || !strings.Contains(heartbeatResponse.Payload, `status="success"`) {
		t.Fatalf("heartbeat response = %#v", heartbeatResponse)
	}
	if commandResponse == nil || commandResponse.ErrorString == "" || !strings.Contains(commandResponse.Payload, mid.UnsupportedCode) {
		t.Fatalf("command response = %#v", commandResponse)
	}
	if strings.Contains(commandResponse.Payload, "touch /tmp/never") || commandResponse.Name == command.Name {
		t.Fatalf("command executable content was reflected into result: %#v", commandResponse)
	}
}

func TestClientRejectsUnsafeConfiguration(t *testing.T) {
	urls := []string{
		"",
		"http://example.service-now.com",
		"https://user:pass@example.service-now.com",
		"https://example.service-now.com/path",
		"https://example.service-now.com?query",
		"https://example.service-now.com#fragment",
	}
	for _, instanceURL := range urls {
		if _, err := mid.NewClient(instanceURL, testUsername, testPassword, nil); err == nil {
			t.Fatalf("unsafe URL %q was accepted", instanceURL)
		}
	}
	if _, err := mid.NewClient("https://example.service-now.com", "bad:user", testPassword, nil); err == nil {
		t.Fatal("username containing a colon was accepted")
	}
	if _, err := mid.NewClient("https://example.service-now.com", testUsername, "", nil); err == nil {
		t.Fatal("empty password was accepted")
	}
	if _, err := mid.AgentName("bad^encoded-query"); err == nil {
		t.Fatal("MID name capable of encoded-query injection was accepted")
	}
}

func TestClientRefusesRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetRequests.Add(1)
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/stolen", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	unsafeClient := redirect.Client()
	unsafeClient.CheckRedirect = nil
	client, err := mid.NewClient(redirect.URL, testUsername, testPassword, unsafeClient)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Poll(context.Background(), mid.AgentPrefix+"redirect-test", 1)
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("redirect error = %v", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatalf("redirect target received %d credential-bearing requests", targetRequests.Load())
	}
}

func TestClientBoundsSOAPResponsesAndReportsFaults(t *testing.T) {
	tests := []struct {
		name     string
		response func(http.ResponseWriter)
		want     string
		notWant  string
	}{
		{
			name: "oversize",
			response: func(writer http.ResponseWriter) {
				writer.Header().Set("Content-Type", "text/xml")
				_, _ = writer.Write([]byte(strings.Repeat("x", (2<<20)+1)))
			},
			want: "exceeds",
		},
		{
			name: "depth",
			response: func(writer http.ResponseWriter) {
				writer.Header().Set("Content-Type", "text/xml")
				_, _ = fmt.Fprint(writer, strings.Repeat("<a>", 70)+strings.Repeat("</a>", 70))
			},
			want: "depth",
		},
		{
			name: "record count",
			response: func(writer http.ResponseWriter) {
				writer.Header().Set("Content-Type", "text/xml")
				_, _ = fmt.Fprint(writer, `<?xml version="1.0"?><Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/"><Body><getRecordsResponse><getRecordsResult><sys_id>00000000000000000000000000000001</sys_id></getRecordsResult><getRecordsResult><sys_id>00000000000000000000000000000002</sys_id></getRecordsResult></getRecordsResponse></Body></Envelope>`)
			},
			want: "returned 2 ECC records",
		},
		{
			name: "fault",
			response: func(writer http.ResponseWriter) {
				writer.Header().Set("Content-Type", "text/xml")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprint(writer, `<?xml version="1.0"?><Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/"><Body><Fault><faultcode>Server</faultcode><faultstring>super-secret-reflection</faultstring></Fault></Body></Envelope>`)
			},
			want:    "SOAP fault",
			notWant: "super-secret-reflection",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				test.response(writer)
			}))
			defer server.Close()
			client, err := mid.NewClient(server.URL, testUsername, testPassword, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Poll(context.Background(), mid.AgentPrefix+"bounds", 1)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if test.notWant != "" && strings.Contains(err.Error(), test.notWant) {
				t.Fatalf("error reflected sensitive SOAP fault text: %v", err)
			}
		})
	}
}

func TestClientRejectsWrongBasicCredential(t *testing.T) {
	fixture := eccsim.New(testUsername, testPassword)
	server := httptest.NewTLSServer(fixture)
	defer server.Close()
	client, err := mid.NewClient(server.URL, testUsername, "wrong-password", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Poll(context.Background(), mid.AgentPrefix+"auth", 1)
	if err == nil || !strings.Contains(err.Error(), "SOAP fault") {
		t.Fatalf("wrong credential error = %v", err)
	}
}

func TestDispatchDeniesExecutableAndUnknownTopics(t *testing.T) {
	for _, topic := range []string{"Command", "SSHCommand", "PowerShell", "Javascript", "Groovy", "UnknownProbe"} {
		record := mid.Record{
			SysID:      "0123456789abcdef0123456789abcdef",
			Agent:      mid.AgentPrefix + "deny-test",
			Topic:      topic,
			Name:       "rm -rf /",
			Source:     "192.0.2.10",
			Queue:      mid.QueueOutput,
			State:      mid.StateProcessing,
			Parameters: "script=malicious",
			Payload:    `<script>malicious()</script>`,
		}
		result, err := mid.Dispatch(record, "test")
		if err != nil {
			t.Fatal(err)
		}
		if result.ErrorString == "" || !strings.Contains(result.Payload, mid.UnsupportedCode) {
			t.Fatalf("topic %q result = %#v", topic, result)
		}
		if result.Name == record.Name {
			t.Fatalf("topic %q executable name was reflected into the result", topic)
		}
		if strings.Contains(result.Payload, "rm -rf") || strings.Contains(result.Payload, "malicious()") {
			t.Fatalf("topic %q executable content was copied into the structured result: %q", topic, result.Payload)
		}
	}
}
