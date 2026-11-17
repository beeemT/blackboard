package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/beeemT/blackboard/internal/domain"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "db")
	var out, err bytes.Buffer
	code := Run(context.Background(), []string{"--db", p, "apply"}, strings.NewReader(`{"operation":"topic.create","operation_id":"1","actor":"omp:a","id":"billing","title":"Billing","goal":"Ship billing","priority":3}`), &out, &err)
	if code != 0 {
		t.Fatal(err.String())
	}
	out.Reset()
	code = Run(context.Background(), []string{"--db", p, "status", "billing"}, nil, &out, &err)
	if code != 0 || !json.Valid(out.Bytes()) || !strings.Contains(out.String(), "Ship billing") {
		t.Fatal(out.String(), err.String())
	}
}
func TestRejectUnknownJSON(t *testing.T) {
	var out, err bytes.Buffer
	code := Run(context.Background(), []string{"--db", filepath.Join(t.TempDir(), "db"), "apply"}, strings.NewReader(`{"operation":"x","surprise":true}`), &out, &err)
	if code != 1 || out.Len() != 0 || !json.Valid(err.Bytes()) {
		t.Fatal(code, out.String(), err.String())
	}
}

func TestFilteredHealth(t *testing.T) {
	v := domain.Empty()
	v.Topics = append(v.Topics, domain.Topic{ID: "t"})
	v.Work = append(v.Work, domain.Work{ID: "w", Ref: "https://gitlab.example/g/p/-/merge_requests/1"})
	v.Links = append(v.Links, domain.Link{TopicID: "t", WorkID: "w"})
	v.Observations = append(v.Observations, domain.Observation{Source: "collector", Key: "mr-check", State: "error", Data: map[string]string{"target": v.Work[0].Ref}})
	r, e := filter(v, "t")
	if e != nil || len(r.Observations) != 1 {
		t.Fatal("lost failure health", r, e)
	}
}
