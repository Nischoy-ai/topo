package controller

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/pkg/model"
)

// ErrJobNotFound indicates a job ID unknown to the store.
var ErrJobNotFound = errors.New("job not found")

const jobIDBytes = 16 // 128 bits

type jobStatus string

const (
	jobPending    jobStatus = "pending"
	jobDispatched jobStatus = "dispatched"
	jobSucceeded  jobStatus = "succeeded"
	jobFailed     jobStatus = "failed"
)

type jobRecord struct {
	collectorID string
	jobType     model.JobType
	status      jobStatus
	createdAt   time.Time
	completedAt time.Time
	resultError string
}

// JobStore queues jobs for collector-initiated polling and tracks their
// lifecycle: pending, dispatched (returned by a poll), and succeeded or
// failed (a result has been reported). Like TokenStore and
// HeartbeatStore, it is in-memory only and does not survive a controller
// restart — an operator must resubmit a job that was queued but never
// polled before a restart.
type JobStore struct {
	mu   sync.Mutex
	jobs map[string]*jobRecord
}

func NewJobStore() *JobStore { return &JobStore{jobs: map[string]*jobRecord{}} }

// Enqueue queues a new job of jobType for collectorID, returning its
// assigned ID.
func (s *JobStore) Enqueue(collectorID string, jobType model.JobType) (model.Job, error) {
	buf := make([]byte, jobIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return model.Job{}, fmt.Errorf("generate job id: %w", err)
	}
	id := hex.EncodeToString(buf)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[id] = &jobRecord{collectorID: collectorID, jobType: jobType, status: jobPending, createdAt: now}
	return model.Job{JobID: id, CollectorID: collectorID, Type: jobType, CreatedAt: now}, nil
}

// Poll returns every pending job queued for collectorID, oldest first,
// and marks each one dispatched so a later poll does not redeliver it. A
// collector that crashes after polling but before reporting a result
// loses that job; there is no redelivery of an already-dispatched job.
func (s *JobStore) Poll(collectorID string) []model.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Job
	for id, r := range s.jobs {
		if r.collectorID != collectorID || r.status != jobPending {
			continue
		}
		r.status = jobDispatched
		out = append(out, model.Job{JobID: id, CollectorID: r.collectorID, Type: r.jobType, CreatedAt: r.createdAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Result records collectorID's report for jobID. It fails if the job is
// unknown, was queued for a different collector, or is not currently
// awaiting a result (already reported, or never polled) — a collector
// can only ever report the outcome of a job it was actually dispatched.
func (s *JobStore) Result(jobID, collectorID string, result model.JobResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	if r.collectorID != collectorID {
		return errors.New("job was not queued for this collector")
	}
	if r.status != jobDispatched {
		return errors.New("job is not awaiting a result")
	}
	if result.Success {
		r.status = jobSucceeded
	} else {
		r.status = jobFailed
	}
	r.completedAt = time.Now()
	r.resultError = result.Error
	return nil
}

// HasOutstanding reports whether collectorID has a job of jobType that is
// pending or dispatched (i.e. not yet resulted). The scheduler uses this to
// avoid queuing a second scheduled job while an earlier one is still
// outstanding, so a slow or temporarily offline collector catches up with
// one job on its next poll rather than an ever-growing backlog.
func (s *JobStore) HasOutstanding(collectorID string, jobType model.JobType) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.jobs {
		if r.collectorID == collectorID && r.jobType == jobType && (r.status == jobPending || r.status == jobDispatched) {
			return true
		}
	}
	return false
}

// Status returns jobID's current lifecycle state.
func (s *JobStore) Status(jobID string) (model.JobStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.jobs[jobID]
	if !ok {
		return model.JobStatus{}, ErrJobNotFound
	}
	return model.JobStatus{
		JobID:       jobID,
		CollectorID: r.collectorID,
		Type:        r.jobType,
		Status:      string(r.status),
		CreatedAt:   r.createdAt,
		CompletedAt: r.completedAt,
		Error:       r.resultError,
	}, nil
}
