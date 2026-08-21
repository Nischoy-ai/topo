package controller

import (
	"errors"
	"testing"

	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestJobStoreEnqueueThenPoll(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID == "" || job.CollectorID != "collector-1" || job.Type != model.JobTypeDiscover {
		t.Fatalf("job = %+v", job)
	}

	polled := s.Poll("collector-1")
	if len(polled) != 1 || polled[0].JobID != job.JobID {
		t.Fatalf("polled = %+v", polled)
	}

	// A second poll must not redeliver the same job.
	if again := s.Poll("collector-1"); len(again) != 0 {
		t.Fatalf("second poll = %+v, want none", again)
	}
}

func TestJobStorePollOnlyReturnsThatCollectorsJobs(t *testing.T) {
	s := NewJobStore()
	if _, err := s.Enqueue("collector-1", model.JobTypeDiscover); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue("collector-2", model.JobTypeDiscover); err != nil {
		t.Fatal(err)
	}

	polled := s.Poll("collector-1")
	if len(polled) != 1 || polled[0].CollectorID != "collector-1" {
		t.Fatalf("polled = %+v", polled)
	}
}

func TestJobStoreResultThenStatus(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	s.Poll("collector-1")

	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: true}); err != nil {
		t.Fatal(err)
	}

	status, err := s.Status(job.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", status.Status)
	}
}

func TestJobStoreResultRejectsWrongCollector(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	s.Poll("collector-1")

	if err := s.Result(job.JobID, "collector-2", model.JobResult{Success: true}); err == nil {
		t.Fatal("expected a result reported by a different collector to be rejected")
	}
}

func TestJobStoreResultRejectsUndispatchedJob(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	// Never polled, so never dispatched.
	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: true}); err == nil {
		t.Fatal("expected a result for a never-dispatched job to be rejected")
	}
}

func TestJobStoreResultRejectsDoubleReport(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	s.Poll("collector-1")
	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: true}); err == nil {
		t.Fatal("expected a second result report for the same job to be rejected")
	}
}

func TestJobStoreResultUnknownJob(t *testing.T) {
	s := NewJobStore()
	err := s.Result("nonexistent", "collector-1", model.JobResult{Success: true})
	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("error = %v, want ErrJobNotFound", err)
	}
}

func TestJobStoreStatusUnknownJob(t *testing.T) {
	s := NewJobStore()
	if _, err := s.Status("nonexistent"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("error = %v, want ErrJobNotFound", err)
	}
}

func TestJobStoreHasOutstanding(t *testing.T) {
	s := NewJobStore()
	if s.HasOutstanding("collector-1", model.JobTypeDiscover) {
		t.Fatal("empty store must have no outstanding jobs")
	}

	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasOutstanding("collector-1", model.JobTypeDiscover) {
		t.Fatal("expected a pending job to count as outstanding")
	}
	if s.HasOutstanding("collector-2", model.JobTypeDiscover) {
		t.Fatal("a job queued for a different collector must not count")
	}

	s.Poll("collector-1")
	if !s.HasOutstanding("collector-1", model.JobTypeDiscover) {
		t.Fatal("expected a dispatched-but-unresulted job to still count as outstanding")
	}

	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: true}); err != nil {
		t.Fatal(err)
	}
	if s.HasOutstanding("collector-1", model.JobTypeDiscover) {
		t.Fatal("a job with a reported result must no longer count as outstanding")
	}
}

func TestJobStoreFailedResult(t *testing.T) {
	s := NewJobStore()
	job, err := s.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	s.Poll("collector-1")
	if err := s.Result(job.JobID, "collector-1", model.JobResult{Success: false, Error: "discovery unavailable"}); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status(job.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "failed" || status.Error != "discovery unavailable" {
		t.Fatalf("status = %+v", status)
	}
}
