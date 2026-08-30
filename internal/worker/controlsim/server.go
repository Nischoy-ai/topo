// Package controlsim provides a deterministic in-memory implementation of the
// Slice A ServiceNow scoped REST contract. It proves Topo's worker behavior in
// CI; it is not represented as evidence about ServiceNow itself.
package controlsim

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/internal/worker"
	"github.com/Nischoy-ai/topo/pkg/publisher/servicenow"
)

const maxBodyBytes = 2 << 20

type Config struct {
	Token         string
	LeaseDuration time.Duration
	SuccessRawTTL time.Duration
	FailureRawTTL time.Duration
	Now           func() time.Time
}

type Server struct {
	mu            sync.Mutex
	token         string
	leaseDuration time.Duration
	successRawTTL time.Duration
	failureRawTTL time.Duration
	now           func() time.Time
	nextID        int
	workers       map[string]*WorkerRecord
	runs          map[string]*RunRecord
	tasks         map[string]*TaskRecord
	results       map[string]*ResultRecord
	schedules     map[string]*Schedule
	items         map[string]string
	relations     map[string]string
	deliveries    []IREDelivery
}

type WorkerRecord struct {
	ID            string
	BootID        string
	WorkerPool    string
	SiteID        string
	Version       string
	Capabilities  []string
	PolicyDigest  string
	LastHeartbeat time.Time
}

type RunRecord struct {
	ID            string
	Trigger       string
	State         string
	TaskIDs       []string
	StartedAt     time.Time
	CompletedAt   time.Time
	Assets        int
	Relationships int
	Attempts      int
	Error         string
}

type TaskRecord struct {
	ID              string
	RunID           string
	WorkerPool      string
	Operation       string
	ProfileID       string
	ProfileRevision int
	State           string
	Attempt         int
	AttemptID       string
	WorkerID        string
	BootID          string
	LeaseDigest     string
	LeaseExpiresAt  time.Time
	Deadline        time.Time
	ChunkCount      int
	Error           string
}

type ResultRecord struct {
	TaskID      string
	AttemptID   string
	ChunkNumber int
	ChunkCount  int
	Checksum    string
	Payload     []byte
	CreatedAt   time.Time
	ProcessedAt time.Time
	Terminal    string
	Deleted     bool
}

type Schedule struct {
	ID         string
	WorkerPool string
	Interval   time.Duration
	NextRunAt  time.Time
	Active     bool
}

type IREDelivery struct {
	RunID          string
	TaskID         string
	AttemptID      string
	Preflighted    bool
	Applied        bool
	ItemOperations []string
	RelationOps    []string
	CompletedAt    time.Time
}

type Snapshot struct {
	Workers    []WorkerRecord
	Runs       []RunRecord
	Tasks      []TaskRecord
	Results    []ResultRecord
	Deliveries []IREDelivery
	Items      int
	Relations  int
}

func New(config Config) *Server {
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = time.Minute
	}
	if config.SuccessRawTTL == 0 {
		config.SuccessRawTTL = 24 * time.Hour
	}
	if config.FailureRawTTL == 0 {
		config.FailureRawTTL = 7 * 24 * time.Hour
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Server{
		token:         config.Token,
		leaseDuration: config.LeaseDuration,
		successRawTTL: config.SuccessRawTTL,
		failureRawTTL: config.FailureRawTTL,
		now:           config.Now,
		workers:       map[string]*WorkerRecord{},
		runs:          map[string]*RunRecord{},
		tasks:         map[string]*TaskRecord{},
		results:       map[string]*ResultRecord{},
		schedules:     map[string]*Schedule{},
		items:         map[string]string{},
		relations:     map[string]string{},
	}
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) RunNow(workerPool string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createRunLocked("manual", workerPool)
}

func (s *Server) UpsertSchedule(schedule Schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules[schedule.ID] = &schedule
}

func (s *Server) EnqueueDue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	ids := make([]string, 0)
	for _, schedule := range s.schedules {
		if !schedule.Active || schedule.NextRunAt.After(now) {
			continue
		}
		ids = append(ids, s.createRunLocked("scheduled", schedule.WorkerPool))
		schedule.NextRunAt = now.Add(schedule.Interval)
	}
	return ids
}

func (s *Server) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	deleted := 0
	for _, result := range s.results {
		if result.Deleted || result.ProcessedAt.IsZero() {
			continue
		}
		ttl := s.failureRawTTL
		if result.Terminal == "complete" {
			ttl = s.successRawTTL
		}
		if !now.Before(result.ProcessedAt.Add(ttl)) {
			result.Payload = nil
			result.Deleted = true
			deleted++
		}
	}
	return deleted
}

func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var snapshot Snapshot
	for _, record := range s.workers {
		copy := *record
		copy.Capabilities = append([]string(nil), record.Capabilities...)
		snapshot.Workers = append(snapshot.Workers, copy)
	}
	for _, record := range s.runs {
		copy := *record
		copy.TaskIDs = append([]string(nil), record.TaskIDs...)
		snapshot.Runs = append(snapshot.Runs, copy)
	}
	for _, record := range s.tasks {
		snapshot.Tasks = append(snapshot.Tasks, *record)
	}
	for _, record := range s.results {
		copy := *record
		copy.Payload = append([]byte(nil), record.Payload...)
		snapshot.Results = append(snapshot.Results, copy)
	}
	for _, delivery := range s.deliveries {
		copy := delivery
		copy.ItemOperations = append([]string(nil), delivery.ItemOperations...)
		copy.RelationOps = append([]string(nil), delivery.RelationOps...)
		snapshot.Deliveries = append(snapshot.Deliveries, copy)
	}
	snapshot.Items = len(s.items)
	snapshot.Relations = len(s.relations)
	sort.Slice(snapshot.Runs, func(i, j int) bool { return snapshot.Runs[i].ID < snapshot.Runs[j].ID })
	sort.Slice(snapshot.Tasks, func(i, j int) bool { return snapshot.Tasks[i].ID < snapshot.Tasks[j].ID })
	return snapshot
}

func (s *Server) createRunLocked(trigger, workerPool string) string {
	s.nextID++
	runID := fmt.Sprintf("run-%08d", s.nextID)
	s.nextID++
	taskID := fmt.Sprintf("task-%08d", s.nextID)
	now := s.now().UTC()
	s.runs[runID] = &RunRecord{ID: runID, Trigger: trigger, State: "ready", TaskIDs: []string{taskID}, StartedAt: now}
	s.tasks[taskID] = &TaskRecord{
		ID:              taskID,
		RunID:           runID,
		WorkerPool:      workerPool,
		Operation:       worker.OperationLocalV1,
		ProfileID:       "local-v1",
		ProfileRevision: 1,
		State:           "ready",
	}
	return runID
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.token == "" || r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/api/x_664635_topo/v1/tasks/workers/register":
		s.register(w, r)
	case "/api/x_664635_topo/v1/tasks/workers/heartbeat":
		s.heartbeat(w, r)
	case "/api/x_664635_topo/v1/tasks/claim":
		s.claim(w, r)
	default:
		s.taskResource(w, r)
	}
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var request worker.RegisterRequest
	if !decode(w, r, &request) {
		return
	}
	if request.SchemaVersion != worker.ContractVersion || request.BootID == "" || request.WorkerPool == "" || request.SiteID == "" || len(request.Capabilities) != 1 || request.Capabilities[0] != worker.OperationLocalV1 {
		http.Error(w, `{"error":"invalid registration"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("worker-%08d", s.nextID)
	s.workers[id] = &WorkerRecord{ID: id, BootID: request.BootID, WorkerPool: request.WorkerPool, SiteID: request.SiteID, Version: request.Version, Capabilities: append([]string(nil), request.Capabilities...), PolicyDigest: request.PolicyDigest, LastHeartbeat: s.now().UTC()}
	s.mu.Unlock()
	encode(w, http.StatusCreated, worker.RegisterResponse{WorkerID: id})
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var request worker.HeartbeatRequest
	if !decode(w, r, &request) {
		return
	}
	s.mu.Lock()
	record, ok := s.workers[request.WorkerID]
	if ok && record.BootID == request.BootID {
		record.LastHeartbeat = s.now().UTC()
	}
	s.mu.Unlock()
	if !ok || record.BootID != request.BootID {
		http.Error(w, `{"error":"unknown worker"}`, http.StatusForbidden)
		return
	}
	encode(w, http.StatusOK, worker.HeartbeatResponse{CancelAttemptIDs: []string{}})
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var request worker.ClaimRequest
	if !decode(w, r, &request) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.workers[request.WorkerID]
	if !ok || record.BootID != request.BootID || len(request.Capabilities) != 1 || request.Capabilities[0] != worker.OperationLocalV1 {
		http.Error(w, `{"error":"invalid worker claim"}`, http.StatusForbidden)
		return
	}
	now := s.now().UTC()
	for _, task := range s.tasks {
		if task.State == "leased" || task.State == "results_received" {
			if !task.LeaseExpiresAt.After(now) {
				task.State = "ready"
				task.WorkerID = ""
				task.BootID = ""
				task.LeaseDigest = ""
				task.AttemptID = ""
			}
		}
	}
	ids := make([]string, 0, len(s.tasks))
	for id, task := range s.tasks {
		if task.State == "ready" && task.WorkerPool == record.WorkerPool {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		encode(w, http.StatusOK, worker.ClaimResponse{})
		return
	}
	task := s.tasks[ids[0]]
	token := randomHex()
	tokenDigest := sha256.Sum256([]byte(token))
	task.Attempt++
	task.AttemptID = fmt.Sprintf("attempt-%08d", task.Attempt)
	task.WorkerID = record.ID
	task.BootID = record.BootID
	task.LeaseDigest = hex.EncodeToString(tokenDigest[:])
	task.LeaseExpiresAt = now.Add(s.leaseDuration)
	task.Deadline = task.LeaseExpiresAt
	task.State = "leased"
	run := s.runs[task.RunID]
	run.State = "running"
	run.Attempts++
	encode(w, http.StatusOK, worker.ClaimResponse{Task: &worker.Task{
		TaskID:          task.ID,
		RunID:           task.RunID,
		AttemptID:       task.AttemptID,
		LeaseToken:      token,
		LeaseExpiresAt:  task.LeaseExpiresAt,
		Operation:       task.Operation,
		ProfileID:       task.ProfileID,
		ProfileRevision: task.ProfileRevision,
		Deadline:        task.Deadline,
	}})
}

func (s *Server) taskResource(w http.ResponseWriter, r *http.Request) {
	prefix := "/api/x_664635_topo/v1/tasks/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, prefix)
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	switch parts[1] {
	case "results":
		s.result(w, r, parts[0])
	case "complete":
		s.complete(w, r, parts[0])
	case "renew":
		s.renew(w, r, parts[0])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) renew(w http.ResponseWriter, r *http.Request, taskID string) {
	var request worker.RenewRequest
	if !decode(w, r, &request) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.validLeaseLocked(taskID, request.WorkerID, request.BootID, request.AttemptID, request.LeaseToken)
	if !ok {
		http.Error(w, `{"error":"invalid or expired lease"}`, http.StatusConflict)
		return
	}
	task.LeaseExpiresAt = s.now().UTC().Add(s.leaseDuration)
	task.Deadline = task.LeaseExpiresAt
	encode(w, http.StatusOK, worker.RenewResponse{LeaseExpiresAt: task.LeaseExpiresAt})
}

func (s *Server) result(w http.ResponseWriter, r *http.Request, taskID string) {
	var request worker.ResultChunkRequest
	if !decode(w, r, &request) {
		return
	}
	if request.ChunkNumber != 0 || request.ChunkCount != 1 || len(request.ObservationJSON) == 0 || len(request.ObservationJSON) > 1<<20 {
		http.Error(w, `{"error":"invalid result bounds"}`, http.StatusBadRequest)
		return
	}
	payload := []byte(request.ObservationJSON)
	sum := sha256.Sum256(payload)
	if request.Checksum != hex.EncodeToString(sum[:]) {
		http.Error(w, `{"error":"checksum mismatch"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.validLeaseLocked(taskID, request.WorkerID, request.BootID, request.AttemptID, request.LeaseToken)
	if !ok {
		http.Error(w, `{"error":"invalid or expired lease"}`, http.StatusConflict)
		return
	}
	key := resultKey(taskID, request.AttemptID, request.ChunkNumber)
	if existing, found := s.results[key]; found {
		if existing.Checksum != request.Checksum || !bytes.Equal(existing.Payload, payload) {
			http.Error(w, `{"error":"idempotency conflict"}`, http.StatusConflict)
			return
		}
		encode(w, http.StatusOK, worker.ResultChunkResponse{Accepted: true, Duplicate: true})
		return
	}
	s.results[key] = &ResultRecord{TaskID: taskID, AttemptID: request.AttemptID, ChunkNumber: request.ChunkNumber, ChunkCount: request.ChunkCount, Checksum: request.Checksum, Payload: append([]byte(nil), payload...), CreatedAt: s.now().UTC()}
	task.State = "results_received"
	task.ChunkCount = 1
	encode(w, http.StatusCreated, worker.ResultChunkResponse{Accepted: true})
}

func (s *Server) complete(w http.ResponseWriter, r *http.Request, taskID string) {
	var request worker.CompleteRequest
	if !decode(w, r, &request) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.validLeaseLocked(taskID, request.WorkerID, request.BootID, request.AttemptID, request.LeaseToken)
	if !ok {
		http.Error(w, `{"error":"invalid or expired lease"}`, http.StatusConflict)
		return
	}
	run := s.runs[task.RunID]
	if !request.Success {
		if request.Failure == nil || request.Failure.Code == "" || len(request.Failure.Message) > 4096 {
			http.Error(w, `{"error":"invalid failure"}`, http.StatusBadRequest)
			return
		}
		task.State = "failed"
		task.Error = request.Failure.Code
		run.State = "failed"
		run.Error = request.Failure.Code
		run.CompletedAt = s.now().UTC()
		encode(w, http.StatusOK, worker.CompleteResponse{TaskState: task.State, RunState: run.State})
		return
	}
	if request.ChunkCount != 1 || task.State != "results_received" {
		http.Error(w, `{"error":"result chunks incomplete"}`, http.StatusConflict)
		return
	}
	result := s.results[resultKey(taskID, request.AttemptID, 0)]
	envelopes, err := servicenow.DecodeJSONLines(bytes.NewReader(result.Payload))
	if err != nil {
		s.failIRELocked(task, run, result, err)
		http.Error(w, `{"error":"observation validation failed"}`, http.StatusUnprocessableEntity)
		return
	}
	preview := servicenow.Publisher{Config: servicenow.Config{InstanceURL: "https://simulator.invalid", DiscoverySource: "Nischoy Topo", DryRun: true}}
	mapped, err := preview.Preview(r.Context(), envelopes)
	if err != nil {
		s.failIRELocked(task, run, result, err)
		http.Error(w, `{"error":"IRE preflight failed"}`, http.StatusUnprocessableEntity)
		return
	}
	payload := mapped.(servicenow.IREPayload)
	delivery := IREDelivery{RunID: run.ID, TaskID: task.ID, AttemptID: task.AttemptID, Preflighted: true, Applied: true, CompletedAt: s.now().UTC()}
	itemKeys := make([]string, len(payload.Items))
	for index, item := range payload.Items {
		key := item.ClassName + "\x00" + item.SourceInfo.SourceName + "\x00" + item.SourceInfo.SourceNativeKey
		itemKeys[index] = key
		if _, exists := s.items[key]; exists {
			delivery.ItemOperations = append(delivery.ItemOperations, "NO_CHANGE")
		} else {
			s.items[key] = randomHex()
			delivery.ItemOperations = append(delivery.ItemOperations, "INSERT")
		}
	}
	for _, relation := range payload.Relations {
		key := relation.Type + "\x00" + itemKeys[relation.Parent] + "\x00" + itemKeys[relation.Child]
		if _, exists := s.relations[key]; exists {
			delivery.RelationOps = append(delivery.RelationOps, "NO_CHANGE")
		} else {
			s.relations[key] = randomHex()
			delivery.RelationOps = append(delivery.RelationOps, "INSERT")
		}
	}
	s.deliveries = append(s.deliveries, delivery)
	task.State = "complete"
	run.State = "complete"
	run.Assets = len(payload.Items)
	run.Relationships = len(payload.Relations)
	run.CompletedAt = s.now().UTC()
	result.ProcessedAt = run.CompletedAt
	result.Terminal = "complete"
	encode(w, http.StatusOK, worker.CompleteResponse{TaskState: task.State, RunState: run.State})
}

func (s *Server) validLeaseLocked(taskID, workerID, bootID, attemptID, leaseToken string) (*TaskRecord, bool) {
	task, ok := s.tasks[taskID]
	if !ok || (task.State != "leased" && task.State != "results_received") || task.WorkerID != workerID || task.BootID != bootID || task.AttemptID != attemptID || !task.LeaseExpiresAt.After(s.now().UTC()) {
		return nil, false
	}
	sum := sha256.Sum256([]byte(leaseToken))
	return task, task.LeaseDigest == hex.EncodeToString(sum[:])
}

func (s *Server) failIRELocked(task *TaskRecord, run *RunRecord, result *ResultRecord, err error) {
	task.State = "failed"
	task.Error = "ire_preflight_failed"
	run.State = "failed"
	run.Error = "ire_preflight_failed"
	run.CompletedAt = s.now().UTC()
	result.ProcessedAt = run.CompletedAt
	result.Terminal = "failed"
	s.deliveries = append(s.deliveries, IREDelivery{RunID: run.ID, TaskID: task.ID, AttemptID: task.AttemptID, Preflighted: true, Applied: false, CompletedAt: run.CompletedAt})
	_ = err
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil || len(body) > maxBodyBytes {
		http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, `{"error":"multiple JSON values"}`, http.StatusBadRequest)
		return false
	}
	return true
}

func encode(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Result any `json:"result"`
	}{Result: value})
}

func resultKey(taskID, attemptID string, chunkNumber int) string {
	return fmt.Sprintf("%s\x00%s\x00%d", taskID, attemptID, chunkNumber)
}

func randomHex() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value)
}
