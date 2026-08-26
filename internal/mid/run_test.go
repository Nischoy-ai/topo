package mid

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type memoryTransport struct {
	record        Record
	responses     []Record
	nextID        int
	failMarkCount int
}

func (t *memoryTransport) Poll(_ context.Context, agent string, limit int) ([]Record, error) {
	if limit < 1 || t.record.Agent != agent || t.record.State != StateReady {
		return nil, nil
	}
	return []Record{t.record}, nil
}

func (t *memoryTransport) Get(_ context.Context, sysID, agent string) (Record, error) {
	if t.record.SysID != sysID || t.record.Agent != agent {
		return Record{}, errors.New("not found")
	}
	return t.record, nil
}

func (t *memoryTransport) Claim(_ context.Context, record Record) (Record, error) {
	if t.record.SysID != record.SysID || t.record.State != StateReady {
		return Record{}, errors.New("not claimable")
	}
	t.record.State = StateProcessing
	return t.record, nil
}

func (t *memoryTransport) FindResponses(_ context.Context, responseTo, agent string) ([]Record, error) {
	var matches []Record
	for _, response := range t.responses {
		if response.ResponseTo == responseTo && response.Agent == agent {
			matches = append(matches, response)
		}
	}
	return matches, nil
}

func (t *memoryTransport) InsertResult(_ context.Context, record Record) (string, error) {
	t.nextID++
	record.SysID = responseID(t.nextID)
	t.responses = append(t.responses, record)
	return record.SysID, nil
}

func (t *memoryTransport) MarkProcessed(_ context.Context, record Record) error {
	if t.failMarkCount > 0 {
		t.failMarkCount--
		return errors.New("fixture state update failed")
	}
	if record.SysID != t.record.SysID || record.State != StateProcessing {
		return errors.New("invalid processed transition")
	}
	t.record.State = StateProcessed
	return nil
}

func TestRunCycleRecoversWithoutDuplicateResult(t *testing.T) {
	state, err := OpenState(t.TempDir(), "recovery")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	agent, _ := AgentName("recovery")
	transport := &memoryTransport{
		record: Record{
			SysID:           "0123456789abcdef0123456789abcdef",
			Agent:           agent,
			Topic:           TopicHeartbeat,
			Name:            "heartbeat",
			Queue:           QueueOutput,
			State:           StateReady,
			AgentCorrelator: "corr-1",
		},
		failMarkCount: 1,
	}
	config := RunConfig{MIDName: "recovery", Version: "test", BatchSize: 1, Transport: transport, State: state}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runCycle(context.Background(), config, agent, logger)
	if len(transport.responses) != 1 || transport.record.State != StateProcessing {
		t.Fatalf("first cycle responses=%d state=%q", len(transport.responses), transport.record.State)
	}
	if entry, err := state.Load(); err != nil || entry == nil || entry.Result == nil || entry.ResponseSysID == "" {
		t.Fatalf("retained recovery journal = %#v, %v", entry, err)
	}
	runCycle(context.Background(), config, agent, logger)
	if len(transport.responses) != 1 {
		t.Fatalf("restart inserted %d responses, want exactly one", len(transport.responses))
	}
	if transport.record.State != StateProcessed {
		t.Fatalf("recovered state = %q", transport.record.State)
	}
	if entry, err := state.Load(); err != nil || entry != nil {
		t.Fatalf("completed recovery journal = %#v, %v", entry, err)
	}
}

func TestRunCycleDeniesUnknownTopicAndCompletesIt(t *testing.T) {
	state, err := OpenState(t.TempDir(), "denial")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	agent, _ := AgentName("denial")
	transport := &memoryTransport{record: Record{
		SysID:      "fedcba9876543210fedcba9876543210",
		Agent:      agent,
		Topic:      "Command",
		Name:       "dangerous command",
		Source:     "192.0.2.1",
		Queue:      QueueOutput,
		State:      StateReady,
		Parameters: "command=do-not-run",
		Payload:    "<script>do-not-run</script>",
	}}
	config := RunConfig{MIDName: "denial", Version: "test", BatchSize: 1, Transport: transport, State: state}
	runCycle(context.Background(), config, agent, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if transport.record.State != StateProcessed || len(transport.responses) != 1 {
		t.Fatalf("state=%q responses=%d", transport.record.State, len(transport.responses))
	}
	response := transport.responses[0]
	if response.ErrorString == "" || response.Topic != "Command" || response.ResponseTo != transport.record.SysID {
		t.Fatalf("denied response = %#v", response)
	}
	if response.Payload == transport.record.Payload {
		t.Fatal("executable payload was reflected as the result")
	}
}

func TestRunCycleStopsOnDuplicateResponses(t *testing.T) {
	state, err := OpenState(t.TempDir(), "duplicates")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	agent, _ := AgentName("duplicates")
	record := Record{
		SysID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Agent: agent,
		Topic: TopicHeartbeat,
		Queue: QueueOutput,
		State: StateReady,
	}
	transport := &memoryTransport{record: record, responses: []Record{
		{SysID: responseID(1), Agent: agent, Topic: TopicHeartbeat, Queue: QueueInput, State: StateReady, ResponseTo: record.SysID},
		{SysID: responseID(2), Agent: agent, Topic: TopicHeartbeat, Queue: QueueInput, State: StateReady, ResponseTo: record.SysID},
	}}
	config := RunConfig{MIDName: "duplicates", Version: "test", BatchSize: 1, Transport: transport, State: state}
	runCycle(context.Background(), config, agent, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if transport.record.State != StateProcessing {
		t.Fatalf("duplicate response state = %q, want processing for operator visibility", transport.record.State)
	}
	if entry, err := state.Load(); err != nil || entry == nil {
		t.Fatalf("duplicate response journal = %#v, %v", entry, err)
	}
}

func responseID(value int) string {
	if value == 1 {
		return "00000000000000000000000000000001"
	}
	return "00000000000000000000000000000002"
}
