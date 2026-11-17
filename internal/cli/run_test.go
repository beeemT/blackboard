package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/beeemT/blackboard/internal/domain"
	"os"
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

func TestDefaultDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BLACKBOARD_DB", "")
	var out, err bytes.Buffer
	if Run(context.Background(), []string{"status"}, nil, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if _, e := os.Stat(filepath.Join(home, ".blackboard", "db")); e != nil {
		t.Fatal(e)
	}
}

func TestDatabaseOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	envPath := filepath.Join(home, "env.db")
	t.Setenv("BLACKBOARD_DB", envPath)
	var out, err bytes.Buffer
	if Run(context.Background(), []string{"status"}, nil, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if _, e := os.Stat(envPath); e != nil {
		t.Fatal(e)
	}
	flagPath := filepath.Join(home, "flag.db")
	if Run(context.Background(), []string{"--db", flagPath, "status"}, nil, &out, &err) != 0 {
		t.Fatal(err.String())
	}
	if _, e := os.Stat(flagPath); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(home, ".blackboard", "db")); !os.IsNotExist(e) {
		t.Fatal("override unexpectedly created default database")
	}
}
