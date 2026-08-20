package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
)

func postSchedule(t *testing.T, h http.Handler, body scheduleRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/schedules", bytes.NewReader(raw))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestCreateScheduleThenPollProducesADueJob(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	w := postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 300})
	if w.Code != http.StatusOK {
		t.Fatalf("create schedule status %d: %s", w.Code, w.Body.String())
	}
	var sched store.Schedule
	if err := json.Unmarshal(w.Body.Bytes(), &sched); err != nil {
		t.Fatal(err)
	}
	if sched.CollectorID != "collector-1" || sched.IntervalSeconds != 300 {
		t.Fatalf("unexpected schedule: %#v", sched)
	}

	// NextRunAt defaults to now, so the very next poll should produce a job.
	r := httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status %d: %s", w.Code, w.Body.String())
	}
	var polled struct {
		Items []struct {
			Type string `json:"type"`
		} `json:"items"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &polled); err != nil {
		t.Fatal(err)
	}
	if polled.Count != 1 || polled.Items[0].Type != "discover" {
		t.Fatalf("got %+v, want exactly one discover job", polled)
	}

	// A second immediate poll must not produce a second job: the schedule
	// was just advanced past now, and the first scheduled job is still
	// outstanding (dispatched, no result reported yet).
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	json.Unmarshal(w.Body.Bytes(), &polled)
	if polled.Count != 0 {
		t.Fatalf("second immediate poll returned %d jobs, want 0", polled.Count)
	}
}

func TestScheduleDoesNotPileUpJobsWhileOneIsOutstanding(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	// A very short interval so the schedule is due again almost immediately.
	if w := postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 60}); w.Code != http.StatusOK {
		t.Fatalf("create schedule status %d: %s", w.Code, w.Body.String())
	}
	// Force the schedule due again right away, simulating time having passed
	// without the collector ever reporting a result for the first job.
	sched, err := s.Store.GetSchedule(context.Background(), "collector-1")
	if err != nil {
		t.Fatal(err)
	}

	pollOnce := func() int {
		r := httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var body struct {
			Count int `json:"count"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		return body.Count
	}
	if got := pollOnce(); got != 1 {
		t.Fatalf("first poll returned %d jobs, want 1", got)
	}

	// Manually rewind NextRunAt into the past, as if the interval had
	// already elapsed again, while the first job is still outstanding.
	sched.NextRunAt = sched.NextRunAt.Add(-time.Hour)
	if err := s.Store.UpsertSchedule(context.Background(), sched); err != nil {
		t.Fatal(err)
	}
	if got := pollOnce(); got != 0 {
		t.Fatalf("poll with an outstanding scheduled job returned %d jobs, want 0 (no pile-up)", got)
	}
}

func TestUpdatingAScheduleReplacesItAndPreservesCreatedAt(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	w := postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 3600})
	var first store.Schedule
	json.Unmarshal(w.Body.Bytes(), &first)

	w = postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 1800})
	if w.Code != http.StatusOK {
		t.Fatalf("update schedule status %d: %s", w.Code, w.Body.String())
	}
	var second store.Schedule
	json.Unmarshal(w.Body.Bytes(), &second)

	if second.IntervalSeconds != 1800 {
		t.Fatalf("interval_seconds = %d, want 1800", second.IntervalSeconds)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("CreatedAt changed on update: got %v, want %v", second.CreatedAt, first.CreatedAt)
	}

	schedules, err := s.Store.ListSchedules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(schedules) != 1 {
		t.Fatalf("got %d schedules after two upserts for the same collector, want 1", len(schedules))
	}
}

func TestScheduleRejectsIntervalOutOfBounds(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	for _, interval := range []int64{0, 59, maxScheduleIntervalSeconds + 1} {
		w := postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: interval})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("interval_seconds=%d: status = %d, want 400", interval, w.Code)
		}
	}
}

func TestScheduleRejectsUnsupportedJobType(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	w := postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", Type: "sweep", IntervalSeconds: 300})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestListSchedulesReturnsEveryCollector(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 300})
	postSchedule(t, h, scheduleRequest{CollectorID: "collector-2", IntervalSeconds: 300})

	r := httptest.NewRequest(http.MethodGet, "/v1/schedules", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var body struct {
		Count int `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
}

func TestDeleteScheduleStopsFutureJobs(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 300})

	r := httptest.NewRequest(http.MethodDelete, "/v1/schedules/collector-1", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}

	if _, err := s.Store.GetSchedule(context.Background(), "collector-1"); err == nil {
		t.Fatal("expected schedule to be gone after delete")
	}

	poll := httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
	poll.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, poll)
	var body struct {
		Count int `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Count != 0 {
		t.Fatalf("poll after delete returned %d jobs, want 0", body.Count)
	}
}

func TestDeleteScheduleUnknownCollectorReturns404(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	r := httptest.NewRequest(http.MethodDelete, "/v1/schedules/nonexistent", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestSchedulesRequireAuth(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/schedules", bytes.NewReader([]byte(`{}`))),
		httptest.NewRequest(http.MethodGet, "/v1/schedules", nil),
		httptest.NewRequest(http.MethodDelete, "/v1/schedules/collector-1", nil),
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401", req.Method, req.URL.Path, w.Code)
		}
	}
}

func TestScheduleCreationIsAudited(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 300})

	entries := getAuditEntries(t, h, "secret")
	if len(entries) != 1 || entries[0].Action != "schedule_created" {
		t.Fatalf("got %+v, want a single schedule_created entry", entries)
	}
	if entries[0].Detail["collector_id"] != "collector-1" || entries[0].Detail["interval_seconds"] != "300" {
		t.Fatalf("unexpected audit detail: %#v", entries[0].Detail)
	}

	postSchedule(t, h, scheduleRequest{CollectorID: "collector-1", IntervalSeconds: 600})
	entries = getAuditEntries(t, h, "secret")
	if len(entries) != 2 || entries[1].Action != "schedule_updated" {
		t.Fatalf("got %+v, want schedule_created then schedule_updated", entries)
	}

	r := httptest.NewRequest(http.MethodDelete, "/v1/schedules/collector-1", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	entries = getAuditEntries(t, h, "secret")
	if len(entries) != 3 || entries[2].Action != "schedule_deleted" {
		t.Fatalf("got %+v, want a trailing schedule_deleted entry", entries)
	}
}
