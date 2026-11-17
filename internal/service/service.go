// Package service owns validation, lifecycle transitions and transactional use cases.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/beeemT/blackboard/internal/domain"
	"strings"
	"time"
)

type Store interface {
	Read(context.Context) (domain.Snapshot, error)
	Apply(context.Context, string, string, func(*domain.Snapshot) (any, error)) (json.RawMessage, error)
}
type Service struct {
	store Store
	now   func() time.Time
}

func New(s Store) *Service { return &Service{s, func() time.Time { return time.Now().UTC() }} }

type Command struct {
	Operation        string               `json:"operation"`
	OperationID      string               `json:"operation_id"`
	Actor            string               `json:"actor"`
	ID               string               `json:"id,omitempty"`
	TopicID          string               `json:"topic_id,omitempty"`
	Title            string               `json:"title,omitempty"`
	Goal             string               `json:"goal,omitempty"`
	Description      string               `json:"description,omitempty"`
	Priority         int                  `json:"priority,omitempty"`
	Status           string               `json:"status,omitempty"`
	Message          string               `json:"message,omitempty"`
	Kind             string               `json:"kind,omitempty"`
	Tool             string               `json:"tool,omitempty"`
	NativeID         string               `json:"native_id,omitempty"`
	Machine          string               `json:"machine,omitempty"`
	Locator          string               `json:"locator,omitempty"`
	SessionID        string               `json:"session_id,omitempty"`
	WorkID           string               `json:"work_id,omitempty"`
	DependsOn        string               `json:"depends_on,omitempty"`
	Ref              string               `json:"ref,omitempty"`
	ExpectedRevision *int                 `json:"expected_revision,omitempty"`
	Observations     []domain.Observation `json:"observations,omitempty"`
}

func (s *Service) Snapshot(ctx context.Context) (domain.Snapshot, error) { return s.store.Read(ctx) }
func invalid(msg string) error                                           { return fmt.Errorf("invalid: %s", msg) }
func (s *Service) Execute(ctx context.Context, c Command) (json.RawMessage, error) {
	if strings.TrimSpace(c.OperationID) == "" || strings.TrimSpace(c.Actor) == "" {
		return nil, invalid("operation_id and actor required")
	}
	b, _ := json.Marshal(c)
	hash := fmt.Sprintf("%x", sha256.Sum256(b))
	return s.store.Apply(ctx, c.OperationID, hash, func(v *domain.Snapshot) (any, error) {
		now := s.now()
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("%x", sha256.Sum256([]byte(c.OperationID)))[:16]
		}
		topic := func() (*domain.Topic, error) {
			for i := range v.Topics {
				if v.Topics[i].ID == c.TopicID {
					t := &v.Topics[i]
					if c.ExpectedRevision != nil && t.Revision != *c.ExpectedRevision {
						return nil, fmt.Errorf("conflict: topic revision changed")
					}
					return t, nil
				}
			}
			return nil, fmt.Errorf("not_found: topic %s", c.TopicID)
		}
		touch := func(t *domain.Topic) { t.Revision++; t.LastActivityAt = now }
		switch c.Operation {
		case "topic.create":
			if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Goal) == "" || c.Priority < 0 || c.Priority > 4 {
				return nil, invalid("title, goal and priority 0..4 required")
			}
			for _, t := range v.Topics {
				if t.ID == id {
					return nil, fmt.Errorf("conflict: topic exists")
				}
			}
			t := domain.Topic{ID: id, Title: c.Title, Goal: c.Goal, Description: c.Description, Status: "active", Priority: c.Priority, Revision: 1, CreatedAt: now, LastActivityAt: now}
			v.Topics = append(v.Topics, t)
			return t, nil
		case "topic.update":
			t, e := topic()
			if e != nil {
				return nil, e
			}
			if c.Title != "" {
				t.Title = c.Title
			}
			if c.Goal != "" {
				t.Goal = c.Goal
			}
			if c.Priority < 0 || c.Priority > 4 {
				return nil, invalid("priority 0..4")
			}
			t.Priority = c.Priority
			t.Description = c.Description
			touch(t)
			return t, nil
		case "topic.status":
			t, e := topic()
			if e != nil {
				return nil, e
			}
			if c.Status != "active" && c.Status != "blocked" && c.Status != "paused" && c.Status != "closed" {
				return nil, invalid("status must be active, blocked, paused or closed")
			}
			if strings.TrimSpace(c.Message) == "" {
				return nil, invalid("status change requires reason/outcome")
			}
			t.Status = c.Status
			if c.Status == "closed" {
				t.Outcome = c.Message
			} else {
				t.Outcome = ""
			}
			touch(t)
			v.Activity = append(v.Activity, domain.Activity{ID: c.OperationID, TopicID: t.ID, Actor: c.Actor, Kind: "status", Message: c.Message, At: now})
			return t, nil
		case "session.register":
			if c.Tool == "" || c.NativeID == "" || c.Machine == "" {
				return nil, invalid("tool, native_id, machine required")
			}
			for i := range v.Sessions {
				x := &v.Sessions[i]
				if x.Tool == c.Tool && x.NativeID == c.NativeID && x.Machine == c.Machine {
					x.LastSeenAt = now
					x.Locator = c.Locator
					return x, nil
				}
				if x.ID == id {
					return nil, fmt.Errorf("conflict: session id exists")
				}
			}
			x := domain.Session{ID: id, Tool: c.Tool, NativeID: c.NativeID, Machine: c.Machine, Locator: c.Locator, LastSeenAt: now}
			v.Sessions = append(v.Sessions, x)
			return x, nil
		case "work.register":
			if c.Ref == "" || c.Kind == "" {
				return nil, invalid("kind and ref required")
			}
			for _, x := range v.Work {
				if x.Kind == c.Kind && x.Ref == c.Ref {
					return x, nil
				}
				if x.ID == id {
					return nil, fmt.Errorf("conflict: work id exists")
				}
			}
			x := domain.Work{ID: id, Kind: c.Kind, Ref: c.Ref, Description: c.Description}
			v.Work = append(v.Work, x)
			return x, nil
		case "link", "unlink":
			t, e := topic()
			if e != nil {
				return nil, e
			}
			if c.SessionID == "" && c.WorkID == "" && c.DependsOn == "" {
				return nil, invalid("session_id, work_id or depends_on required")
			}
			if c.Operation == "unlink" {
				target := domain.Link{TopicID: t.ID, SessionID: c.SessionID, WorkID: c.WorkID, DependsOn: c.DependsOn}
				kept := []domain.Link{}
				removed := false
				for _, l := range v.Links {
					if l == target {
						removed = true
					} else {
						kept = append(kept, l)
					}
				}
				v.Links = kept
				if removed {
					touch(t)
				}
				return map[string]bool{"removed": removed}, nil
			}
			if c.SessionID != "" {
				found := false
				for _, x := range v.Sessions {
					found = found || x.ID == c.SessionID
				}
				if !found {
					return nil, invalid("unknown session_id")
				}
			}
			if c.WorkID != "" {
				found := false
				for _, x := range v.Work {
					found = found || x.ID == c.WorkID
				}
				if !found {
					return nil, invalid("unknown work_id")
				}
			}
			if c.DependsOn != "" {
				if c.DependsOn == t.ID {
					return nil, invalid("self dependency")
				}
				found := false
				for _, x := range v.Topics {
					found = found || x.ID == c.DependsOn
				}
				if !found {
					return nil, invalid("unknown dependency")
				}
				if reaches(v.Links, c.DependsOn, t.ID, map[string]bool{}) {
					return nil, invalid("dependency cycle")
				}
			}
			l := domain.Link{TopicID: t.ID, SessionID: c.SessionID, WorkID: c.WorkID, DependsOn: c.DependsOn}
			for _, x := range v.Links {
				if x == l {
					return l, nil
				}
			}
			v.Links = append(v.Links, l)
			touch(t)
			return l, nil
		case "progress":
			t, e := topic()
			if e != nil {
				return nil, e
			}
			if c.Message == "" {
				return nil, invalid("message required")
			}
			if t.Status == "closed" {
				return nil, invalid("reopen topic before progress")
			}
			a := domain.Activity{ID: c.OperationID, TopicID: t.ID, Actor: c.Actor, Kind: c.Kind, Message: c.Message, At: now}
			v.Activity = append(v.Activity, a)
			touch(t)
			return a, nil
		case "observe":
			for _, o := range c.Observations {
				if o.Source == "" || o.Key == "" || o.ObservedAt.IsZero() || o.ObservedAt.After(now.Add(time.Minute)) {
					return nil, invalid("observation source/key and valid observed_at required")
				}
				found := false
				for i, x := range v.Observations {
					if x.Source == o.Source && x.Key == o.Key {
						if !o.ObservedAt.Before(x.ObservedAt) {
							v.Observations[i] = o
						}
						found = true
						break
					}
				}
				if !found {
					v.Observations = append(v.Observations, o)
				}
			}
			return map[string]int{"observations": len(c.Observations)}, nil
		default:
			return nil, invalid("unknown operation")
		}
	})
}
func reaches(links []domain.Link, from, to string, seen map[string]bool) bool {
	if from == to {
		return true
	}
	if seen[from] {
		return false
	}
	seen[from] = true
	for _, l := range links {
		if l.TopicID == from && l.DependsOn != "" && reaches(links, l.DependsOn, to, seen) {
			return true
		}
	}
	return false
}
