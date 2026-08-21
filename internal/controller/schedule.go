package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// maxScheduleRequestBytes bounds the POST /v1/schedules request body, a
// small fixed-shape object like the job request bodies.
const maxScheduleRequestBytes = 4 << 10

// minScheduleIntervalSeconds and maxScheduleIntervalSeconds bound how often
// a schedule can fire: below the minimum, a misconfigured schedule could
// hammer a collector every poll; above the maximum, "recurring" stops being
// a meaningful distinction from a one-off POST /v1/jobs.
const (
	minScheduleIntervalSeconds = 60
	maxScheduleIntervalSeconds = 7 * 24 * 60 * 60 // one week
)

// scheduleRequest is the POST /v1/schedules request body: create or
// replace a collector's recurring discovery schedule. There is no
// corresponding agent-facing type in pkg/model because a collector never
// sees a schedule directly — it only ever sees the model.Job objects a due
// schedule produces via GET /v1/jobs, identical to a manually created job.
type scheduleRequest struct {
	CollectorID     string        `json:"collector_id"`
	Type            model.JobType `json:"type,omitempty"` // defaults to model.JobTypeDiscover
	IntervalSeconds int64         `json:"interval_seconds"`
}

// createOrUpdateSchedule creates or replaces the recurring discovery
// schedule for a collector. Wrapped in s.auth() like every other
// admin-style action in this project (POST /v1/jobs included). Every call
// — whether it creates a new schedule or replaces an existing one — sets
// NextRunAt to now, so the effect is always "apply this schedule starting
// now": an admin narrowing an interval, for example, does not have to wait
// out whatever was left of the old cadence before it takes effect.
func (s *Server) createOrUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScheduleRequestBytes)
	var req scheduleRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid schedule request: "+err.Error())
		return
	}
	if !enrollment.ValidCollectorID(req.CollectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	if req.Type == "" {
		req.Type = model.JobTypeDiscover
	}
	if req.Type != model.JobTypeDiscover {
		writeError(w, 400, fmt.Sprintf("unsupported job type %q", req.Type))
		return
	}
	if req.IntervalSeconds < minScheduleIntervalSeconds || req.IntervalSeconds > maxScheduleIntervalSeconds {
		writeError(w, 400, fmt.Sprintf("interval_seconds must be between %d and %d", minScheduleIntervalSeconds, maxScheduleIntervalSeconds))
		return
	}

	now := time.Now()
	sched := store.Schedule{
		CollectorID:     req.CollectorID,
		JobType:         req.Type,
		IntervalSeconds: req.IntervalSeconds,
		NextRunAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	action := "schedule_created"
	if existing, err := s.Store.GetSchedule(r.Context(), req.CollectorID); err == nil {
		sched.CreatedAt = existing.CreatedAt
		action = "schedule_updated"
	}
	if err := s.Store.UpsertSchedule(r.Context(), sched); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.recordAudit(r.Context(), action, collectorIdentity(r, "api-key"), map[string]string{
		"collector_id":     sched.CollectorID,
		"job_type":         string(sched.JobType),
		"interval_seconds": fmt.Sprintf("%d", sched.IntervalSeconds),
	})
	writeJSON(w, http.StatusOK, sched)
}

func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.ListSchedules(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	collectorID := r.PathValue("collector_id")
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	if _, err := s.Store.GetSchedule(r.Context(), collectorID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no schedule for this collector")
			return
		}
		writeError(w, 500, err.Error())
		return
	}
	if err := s.Store.DeleteSchedule(r.Context(), collectorID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.recordAudit(r.Context(), "schedule_deleted", collectorIdentity(r, "api-key"), map[string]string{"collector_id": collectorID})
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// maybeEnqueueScheduledJob checks collectorID's recurring schedule, if any,
// and enqueues a job the same way POST /v1/jobs would if it is due. This is
// the only place a schedule ever turns into an actual job — there is no
// background ticker. That matches Topo Agent's collector-initiated-polling
// architecture (see POST /v1/jobs): a schedule for a collector that never
// polls never produces a job, which is fine, since Topo Agent is
// deliberately outbound-only and there is no other way to reach it anyway.
//
// If a job of the schedule's type is already outstanding for this
// collector (queued but not yet resulted), this does nothing — a slow or
// temporarily offline collector catches up with a single job on its next
// poll rather than an ever-growing backlog, and NextRunAt is left
// untouched so the next poll tries again.
func (s *Server) maybeEnqueueScheduledJob(ctx context.Context, collectorID string) {
	sched, err := s.Store.GetSchedule(ctx, collectorID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.Logger.Error("read schedule failed", "collector_id", collectorID, "err", err)
		}
		return
	}
	if sched.NextRunAt.After(time.Now()) {
		return
	}
	if s.Jobs.HasOutstanding(collectorID, sched.JobType) {
		return
	}
	if _, err := s.Jobs.Enqueue(collectorID, sched.JobType); err != nil {
		s.Logger.Error("enqueue scheduled job failed", "collector_id", collectorID, "err", err)
		return
	}
	sched.NextRunAt = time.Now().Add(time.Duration(sched.IntervalSeconds) * time.Second)
	sched.UpdatedAt = time.Now()
	if err := s.Store.UpsertSchedule(ctx, sched); err != nil {
		s.Logger.Error("advance schedule failed", "collector_id", collectorID, "err", err)
	}
}
