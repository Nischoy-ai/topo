// Package controlsim provides a deterministic in-memory implementation of the
// ServiceNow scoped REST contract. It proves Topo's worker behavior in
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
	PoolMaxLeases int
	Now           func() time.Time
}

type Server struct {
	mu             sync.Mutex
	token          string
	leaseDuration  time.Duration
	successRawTTL  time.Duration
	failureRawTTL  time.Duration
	poolMaxLeases  int
	renewAvailable bool
	now            func() time.Time
	nextID         int
	workers        map[string]*WorkerRecord
	runs           map[string]*RunRecord
	tasks          map[string]*TaskRecord
	results        map[string]*ResultRecord
	schedules      map[string]*Schedule
	items          map[string]string
	relations      map[string]string
	deliveries     []IREDelivery
}

type WorkerRecord struct {
	ID             string
	BootID         string
	WorkerPool     string
	SiteID         string
	Version        string
	Capabilities   []string
	PolicyDigest   string
	MaxConcurrency int
	CurrentLeases  int
	LastHeartbeat  time.Time
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
	TargetPartition *worker.TargetPartition
	CancelRequested bool
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
	if config.PoolMaxLeases <= 0 {
		config.PoolMaxLeases = 32
	}
	return &Server{
		token:          config.Token,
		leaseDuration:  config.LeaseDuration,
		successRawTTL:  config.SuccessRawTTL,
		failureRawTTL:  config.FailureRawTTL,
		poolMaxLeases:  config.PoolMaxLeases,
		renewAvailable: true,
		now:            config.Now,
		workers:        map[string]*WorkerRecord{},
		runs:           map[string]*RunRecord{},
		tasks:          map[string]*TaskRecord{},
		results:        map[string]*ResultRecord{},
		schedules:      map[string]*Schedule{},
		items:          map[string]string{},
		relations:      map[string]string{},
	}
}

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) RunNow(workerPool string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createRunLocked("manual", workerPool, 1)
}

// RunNowPartitions is a simulator-only scale fixture. It never adds a
// production operation: every task still uses local.v1 and a test executor
// supplies the synthetic supported observations.
func (s *Server) RunNowPartitions(workerPool string, partitions int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createRunLocked("manual", workerPool, partitions)
}

func (s *Server) SetRenewAvailable(available bool) {
	s.mu.Lock()
	s.renewAvailable = available
	s.mu.Unlock()
}

func (s *Server) CancelRun(runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok || run.State == "complete" || run.State == "failed" || run.State == "cancelled" {
		return false
	}
	active := false
	for _, taskID := range run.TaskIDs {
		task := s.tasks[taskID]
		switch task.State {
		case "ready":
			task.State = "cancelled"
		case "leased", "running", "results_received":
			task.CancelRequested = true
			active = true
		}
	}
	if active {
		run.State = "cancelling"
	} else {
		run.State = "cancelled"
		run.CompletedAt = s.now().UTC()
	}
	return true
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
		ids = append(ids, s.createRunLocked("scheduled", schedule.WorkerPool, 1))
		schedule.NextRunAt = now.Add(schedule.Interval)
	}
	return ids
}

func (s *Server) Cleanup() int {
	return s.CleanupBatch(int(^uint(0) >> 1))
}

func (s *Server) CleanupBatch(limit int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit < 1 {
		return 0
	}
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
			if deleted == limit {
				break
			}
		}
	}
	return deleted
}

func (s *Server) SeedProcessedResults(count int, processedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := 0; index < count; index++ {
		key := fmt.Sprintf("retention\x00%09d", index)
		s.results[key] = &ResultRecord{TaskID: fmt.Sprintf("retained-%09d", index), AttemptID: "attempt-1", Payload: []byte{1}, ProcessedAt: processedAt.UTC(), Terminal: "complete"}
	}
}

func (s *Server) ResultStats() (total, deleted, payloadBytes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, result := range s.results {
		total++
		if result.Deleted {
			deleted++
		}
		payloadBytes += len(result.Payload)
	}
	return total, deleted, payloadBytes
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
		copy := *record
		if record.TargetPartition != nil {
			partition := *record.TargetPartition
			partition.CIDRs = append([]string(nil), record.TargetPartition.CIDRs...)
			copy.TargetPartition = &partition
		}
		snapshot.Tasks = append(snapshot.Tasks, copy)
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

func (s *Server) createRunLocked(trigger, workerPool string, partitions int) string {
	if partitions < 1 || partitions > worker.DefaultMaxPartitions {
		return ""
	}
	s.nextID++
	runID := fmt.Sprintf("run-%08d", s.nextID)
	now := s.now().UTC()
	run := &RunRecord{ID: runID, Trigger: trigger, State: "ready", StartedAt: now}
	s.runs[runID] = run
	for ordinal := 0; ordinal < partitions; ordinal++ {
		s.nextID++
		taskID := fmt.Sprintf("task-%08d", s.nextID)
		var partition *worker.TargetPartition
		if partitions > 1 {
			cidr := fmt.Sprintf("10.%d.%d.%d/32", (ordinal>>16)&255, (ordinal>>8)&255, ordinal&255)
			key := sha256.Sum256([]byte(fmt.Sprintf("simulator-scope\n1\n%s", cidr)))
			partition = &worker.TargetPartition{Key: hex.EncodeToString(key[:]), Ordinal: ordinal, Count: partitions, CIDRs: []string{cidr}}
		}
		run.TaskIDs = append(run.TaskIDs, taskID)
		s.tasks[taskID] = &TaskRecord{
			ID:              taskID,
			RunID:           runID,
			WorkerPool:      workerPool,
			Operation:       worker.OperationLocalV1,
			ProfileID:       "local-v1",
			ProfileRevision: 1,
			State:           "ready",
			Deadline:        now.Add(10 * time.Minute),
			TargetPartition: partition,
		}
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
	if request.MaxConcurrency == 0 {
		request.MaxConcurrency = worker.DefaultMaxConcurrency
	}
	if request.SchemaVersion != worker.ContractVersion || request.BootID == "" || request.WorkerPool == "" || request.SiteID == "" || len(request.Capabilities) != 1 || request.Capabilities[0] != worker.OperationLocalV1 || request.MaxConcurrency < 1 || request.MaxConcurrency > worker.MaxWorkerConcurrency {
		http.Error(w, `{"error":"invalid registration"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.nextID++
	id := fmt.Sprintf("worker-%08d", s.nextID)
	s.workers[id] = &WorkerRecord{ID: id, BootID: request.BootID, WorkerPool: request.WorkerPool, SiteID: request.SiteID, Version: request.Version, Capabilities: append([]string(nil), request.Capabilities...), PolicyDigest: request.PolicyDigest, MaxConcurrency: request.MaxConcurrency, LastHeartbeat: s.now().UTC()}
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
	cancelled := make([]string, 0)
	if ok && record.BootID == request.BootID {
		record.LastHeartbeat = s.now().UTC()
		record.CurrentLeases = s.workerLeaseCountLocked(record.ID)
		for _, task := range s.tasks {
			if task.WorkerID == record.ID && task.BootID == record.BootID && task.CancelRequested && task.AttemptID != "" {
				cancelled = append(cancelled, task.AttemptID)
			}
		}
		sort.Strings(cancelled)
	}
	s.mu.Unlock()
	if !ok || record.BootID != request.BootID {
		http.Error(w, `{"error":"unknown worker"}`, http.StatusForbidden)
		return
	}
	encode(w, http.StatusOK, worker.HeartbeatResponse{CancelAttemptIDs: cancelled})
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	var request worker.ClaimRequest
	if !decode(w, r, &request) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.workers[request.WorkerID]
	if !ok || record.BootID != request.BootID || len(request.Capabilities) != 1 || request.Capabilities[0] != worker.OperationLocalV1 || request.CurrentLeases < 0 || request.CurrentLeases > record.MaxConcurrency {
		http.Error(w, `{"error":"invalid worker claim"}`, http.StatusForbidden)
		return
	}
	now := s.now().UTC()
	for _, task := range s.tasks {
		if task.State == "leased" || task.State == "running" || task.State == "results_received" {
			if !task.LeaseExpiresAt.After(now) {
				if task.CancelRequested {
					task.State = "cancelled"
				} else {
					task.State = "ready"
				}
				task.WorkerID = ""
				task.BootID = ""
				task.LeaseDigest = ""
				task.AttemptID = ""
				task.CancelRequested = false
				s.refreshRunLocked(s.runs[task.RunID])
			}
		}
	}
	record.CurrentLeases = s.workerLeaseCountLocked(record.ID)
	if record.CurrentLeases >= record.MaxConcurrency || s.poolLeaseCountLocked(record.WorkerPool) >= s.poolMaxLeases {
		encode(w, http.StatusOK, worker.ClaimResponse{})
		return
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
	task.AttemptID = fmt.Sprintf("attempt-%s-%08d", task.ID, task.Attempt)
	task.WorkerID = record.ID
	task.BootID = record.BootID
	task.LeaseDigest = hex.EncodeToString(tokenDigest[:])
	task.LeaseExpiresAt = now.Add(s.leaseDuration)
	if task.LeaseExpiresAt.After(task.Deadline) {
		task.LeaseExpiresAt = task.Deadline
	}
	task.State = "leased"
	record.CurrentLeases++
	run := s.runs[task.RunID]
	run.State = "running"
	run.Attempts++
	var partition *worker.TargetPartition
	if task.TargetPartition != nil {
		copy := *task.TargetPartition
		copy.CIDRs = append([]string(nil), task.TargetPartition.CIDRs...)
		partition = &copy
	}
	encode(w, http.StatusOK, worker.ClaimResponse{Task: &worker.Task{
		TaskID:          task.ID,
		RunID:           task.RunID,
		AttemptID:       task.AttemptID,
		LeaseToken:      token,
		LeaseExpiresAt:  task.LeaseExpiresAt,
		Operation:       task.Operation,
		ProfileID:       task.ProfileID,
		ProfileRevision: task.ProfileRevision,
		TargetPartition: partition,
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
	if !s.renewAvailable {
		http.Error(w, `{"error":"renewal unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	task, ok := s.validLeaseLocked(taskID, request.WorkerID, request.BootID, request.AttemptID, request.LeaseToken)
	if !ok {
		http.Error(w, `{"error":"invalid or expired lease"}`, http.StatusConflict)
		return
	}
	if task.CancelRequested {
		encode(w, http.StatusOK, worker.RenewResponse{LeaseExpiresAt: task.LeaseExpiresAt, Cancelled: true})
		return
	}
	task.LeaseExpiresAt = s.now().UTC().Add(s.leaseDuration)
	if task.LeaseExpiresAt.After(task.Deadline) {
		task.LeaseExpiresAt = task.Deadline
	}
	task.State = "running"
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
	if task.CancelRequested {
		http.Error(w, `{"error":"task cancelled"}`, http.StatusConflict)
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
		if task.CancelRequested || request.Failure.Code == "cancelled" {
			task.State = "cancelled"
			task.Error = "cancelled"
		} else {
			task.State = "failed"
			task.Error = request.Failure.Code
		}
		task.CancelRequested = false
		s.refreshRunLocked(run)
		encode(w, http.StatusOK, worker.CompleteResponse{TaskState: task.State, RunState: run.State})
		return
	}
	if request.ChunkCount != 1 || task.State != "results_received" {
		http.Error(w, `{"error":"result chunks incomplete"}`, http.StatusConflict)
		return
	}
	if task.CancelRequested {
		http.Error(w, `{"error":"task cancelled"}`, http.StatusConflict)
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
	run.Assets += len(payload.Items)
	run.Relationships += len(payload.Relations)
	s.refreshRunLocked(run)
	result.ProcessedAt = s.now().UTC()
	result.Terminal = "complete"
	encode(w, http.StatusOK, worker.CompleteResponse{TaskState: task.State, RunState: run.State})
}

func (s *Server) validLeaseLocked(taskID, workerID, bootID, attemptID, leaseToken string) (*TaskRecord, bool) {
	task, ok := s.tasks[taskID]
	if !ok || (task.State != "leased" && task.State != "running" && task.State != "results_received") || task.WorkerID != workerID || task.BootID != bootID || task.AttemptID != attemptID || !task.LeaseExpiresAt.After(s.now().UTC()) {
		return nil, false
	}
	sum := sha256.Sum256([]byte(leaseToken))
	return task, task.LeaseDigest == hex.EncodeToString(sum[:])
}

func (s *Server) workerLeaseCountLocked(workerID string) int {
	count := 0
	for _, task := range s.tasks {
		if task.WorkerID == workerID && (task.State == "leased" || task.State == "running" || task.State == "results_received") {
			count++
		}
	}
	return count
}

func (s *Server) poolLeaseCountLocked(workerPool string) int {
	count := 0
	for _, task := range s.tasks {
		if task.WorkerPool == workerPool && (task.State == "leased" || task.State == "running" || task.State == "results_received") {
			count++
		}
	}
	return count
}

func (s *Server) refreshRunLocked(run *RunRecord) {
	if run == nil {
		return
	}
	active := 0
	failed := 0
	cancelled := 0
	complete := 0
	for _, taskID := range run.TaskIDs {
		switch s.tasks[taskID].State {
		case "complete":
			complete++
		case "failed":
			failed++
		case "cancelled":
			cancelled++
		default:
			active++
		}
	}
	if active > 0 {
		if run.State != "cancelling" {
			run.State = "running"
		}
		return
	}
	if failed > 0 {
		run.State = "failed"
	} else if cancelled > 0 {
		run.State = "cancelled"
	} else if complete == len(run.TaskIDs) {
		run.State = "complete"
	}
	run.CompletedAt = s.now().UTC()
}

func (s *Server) failIRELocked(task *TaskRecord, run *RunRecord, result *ResultRecord, err error) {
	task.State = "failed"
	task.Error = "ire_preflight_failed"
	run.Error = "ire_preflight_failed"
	s.refreshRunLocked(run)
	result.ProcessedAt = s.now().UTC()
	result.Terminal = "failed"
	s.deliveries = append(s.deliveries, IREDelivery{RunID: run.ID, TaskID: task.ID, AttemptID: task.AttemptID, Preflighted: true, Applied: false, CompletedAt: result.ProcessedAt})
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
