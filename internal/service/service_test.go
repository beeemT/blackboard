package service_test

import (
	"context"
	"encoding/json"
	"github.com/beeemT/blackboard/internal/domain"
	"github.com/beeemT/blackboard/internal/repository/sqlite"
	"github.com/beeemT/blackboard/internal/service"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setup(t *testing.T) (*service.Service, *sqlite.Store) {
	t.Helper()
	db, e := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return service.New(db), db
}
func exec(t *testing.T, s *service.Service, c service.Command) json.RawMessage {
	t.Helper()
	c.Actor = "test"
	v, e := s.Execute(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestLifecycleAndObservation(t *testing.T) {
	s, _ := setup(t)
	exec(t, s, service.Command{Operation: "topic.create", OperationID: "a", ID: "t", Title: "Test", Goal: "Finish", Priority: 4})
	before, _ := s.Snapshot(context.Background())
	exec(t, s, service.Command{Operation: "observe", OperationID: "b", Observations: []domain.Observation{{Source: "gitlab", Key: "mr", State: "merged", ObservedAt: time.Now().UTC()}}})
	after, _ := s.Snapshot(context.Background())
	if after.Topics[0].Status != "active" || !after.Topics[0].LastActivityAt.Equal(before.Topics[0].LastActivityAt) {
		t.Fatal("observation mutated intent")
	}
	exec(t, s, service.Command{Operation: "topic.status", OperationID: "c", TopicID: "t", Status: "closed", Message: "verified goal"})
	after, _ = s.Snapshot(context.Background())
	if after.Topics[0].Outcome != "verified goal" {
		t.Fatal(after)
	}
}
func TestIdempotency(t *testing.T) {
	s, _ := setup(t)
	c := service.Command{Operation: "topic.create", OperationID: "same", ID: "t", Title: "Test", Goal: "Finish", Actor: "test"}
	a, e := s.Execute(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Execute(context.Background(), c)
	if e != nil || string(a) != string(b) {
		t.Fatal("retry differs", e)
	}
	c.Title = "changed"
	if _, e = s.Execute(context.Background(), c); e == nil {
		t.Fatal("payload conflict accepted")
	}
}
func TestConcurrentProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	a, e := sqlite.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := sqlite.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	var wg sync.WaitGroup
	for i, s := range []*service.Service{service.New(a), service.New(b)} {
		wg.Add(1)
		go func(i int, s *service.Service) {
			defer wg.Done()
			id := string(rune('a' + i))
			_, e := s.Execute(context.Background(), service.Command{Operation: "topic.create", OperationID: id, Actor: "test", Title: id, Goal: "goal"})
			if e != nil {
				t.Error(e)
			}
		}(i, s)
	}
	wg.Wait()
	v, e := a.Read(context.Background())
	if e != nil || len(v.Topics) != 2 {
		t.Fatal("lost update", v, e)
	}
}
func TestRevisionAndCycle(t *testing.T) {
	s, _ := setup(t)
	for _, id := range []string{"a", "b"} {
		exec(t, s, service.Command{Operation: "topic.create", OperationID: id, ID: id, Title: id, Goal: "goal"})
	}
	exec(t, s, service.Command{Operation: "link", OperationID: "link", TopicID: "a", DependsOn: "b"})
	_, e := s.Execute(context.Background(), service.Command{Operation: "link", OperationID: "cycle", Actor: "x", TopicID: "b", DependsOn: "a"})
	if e == nil {
		t.Fatal("cycle")
	}
	n := 1
	_, e = s.Execute(context.Background(), service.Command{Operation: "topic.status", OperationID: "stale", Actor: "x", TopicID: "a", Status: "closed", Message: "done", ExpectedRevision: &n})
	if e == nil {
		t.Fatal("stale revision")
	}
}

func TestHistoryAndUnlink(t *testing.T) {
	s, _ := setup(t)
	exec(t, s, service.Command{Operation: "topic.create", OperationID: "create", ID: "t", Title: "T", Goal: "G"})
	exec(t, s, service.Command{Operation: "session.register", OperationID: "reg", ID: "s", Tool: "omp", NativeID: "1", Machine: "mac"})
	exec(t, s, service.Command{Operation: "link", OperationID: "link", TopicID: "t", SessionID: "s"})
	exec(t, s, service.Command{Operation: "unlink", OperationID: "unlink", TopicID: "t", SessionID: "s"})
	for _, op := range []string{"p1", "p2"} {
		exec(t, s, service.Command{Operation: "progress", OperationID: op, ID: "same-user-id", TopicID: "t", Message: op})
	}
	v, e := s.Snapshot(context.Background())
	if e != nil || len(v.Links) != 0 || len(v.Activity) != 2 {
		t.Fatal(v, e)
	}
	if v.Activity[0].ID == v.Activity[1].ID {
		t.Fatal("history overwritten")
	}
}
func TestPausedAttention(t *testing.T) {
	if domain.Attention(domain.Topic{Status: "paused", LastActivityAt: time.Now()}, time.Now()) != "paused" {
		t.Fatal("paused topic surfaced as active")
	}
}

func TestObservationsMonotonicAndHeartbeatNotActivity(t *testing.T) {
	s, _ := setup(t)
	exec(t, s, service.Command{Operation: "topic.create", OperationID: "t", ID: "t", Title: "T", Goal: "G"})
	before, _ := s.Snapshot(context.Background())
	now := time.Now().UTC()
	exec(t, s, service.Command{Operation: "observe", OperationID: "fresh", Observations: []domain.Observation{{Source: "git", Key: "x", State: "fresh", ObservedAt: now}}})
	exec(t, s, service.Command{Operation: "observe", OperationID: "stale", Observations: []domain.Observation{{Source: "git", Key: "x", State: "stale", ObservedAt: now.Add(-time.Hour)}}})
	exec(t, s, service.Command{Operation: "session.register", OperationID: "r1", Tool: "omp", NativeID: "1", Machine: "m"})
	exec(t, s, service.Command{Operation: "session.register", OperationID: "r2", Tool: "omp", NativeID: "1", Machine: "m"})
	v, _ := s.Snapshot(context.Background())
	if v.Observations[0].State != "fresh" || len(v.Sessions) != 1 || !v.Topics[0].LastActivityAt.Equal(before.Topics[0].LastActivityAt) {
		t.Fatal(v)
	}
}
