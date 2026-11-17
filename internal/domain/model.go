// Package domain defines Blackboard's coordination model, independent of storage and transport.
package domain

import "time"

type Topic struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Goal           string    `json:"goal"`
	Description    string    `json:"description,omitempty"`
	Status         string    `json:"status"`
	Priority       int       `json:"priority"`
	Outcome        string    `json:"outcome,omitempty"`
	Revision       int       `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	LastActivityAt time.Time `json:"last_meaningful_activity_at"`
}
type Session struct {
	ID         string    `json:"id"`
	Tool       string    `json:"tool"`
	NativeID   string    `json:"native_id"`
	Machine    string    `json:"machine"`
	Locator    string    `json:"locator,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
type Work struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	Description string `json:"description,omitempty"`
}
type Link struct {
	TopicID   string `json:"topic_id"`
	SessionID string `json:"session_id,omitempty"`
	WorkID    string `json:"work_id,omitempty"`
	DependsOn string `json:"depends_on,omitempty"`
}
type Activity struct {
	ID      string    `json:"id"`
	TopicID string    `json:"topic_id"`
	Actor   string    `json:"actor"`
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}
type Observation struct {
	Source     string            `json:"source"`
	Key        string            `json:"key"`
	State      string            `json:"state"`
	Revision   string            `json:"revision,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
	ObservedAt time.Time         `json:"observed_at"`
}
type Snapshot struct {
	Topics       []Topic       `json:"topics"`
	Sessions     []Session     `json:"sessions"`
	Work         []Work        `json:"work"`
	Links        []Link        `json:"links"`
	Activity     []Activity    `json:"activity"`
	Observations []Observation `json:"observations"`
}

func Empty() Snapshot {
	return Snapshot{Topics: []Topic{}, Sessions: []Session{}, Work: []Work{}, Links: []Link{}, Activity: []Activity{}, Observations: []Observation{}}
}

// Attention is a transparent recency category, never a change to explicit priority.
func Attention(t Topic, now time.Time) string {
	if t.Status == "closed" {
		return "closed"
	}
	if t.Status == "paused" {
		return "paused"
	}
	if t.Status == "blocked" {
		return "blocked"
	}
	if now.Sub(t.LastActivityAt) > 14*24*time.Hour {
		if t.Priority >= 3 {
			return "stale-important"
		}
		return "dormant"
	}
	return "active"
}
