package reconcile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeExec struct {
	calls   [][]string
	outputs []string
	err     error
}

func (f *fakeExec) Run(ctx context.Context, n string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{n}, args...))
	if f.err != nil {
		return nil, f.err
	}
	if len(f.outputs) == 0 {
		return nil, errors.New("no output")
	}
	s := f.outputs[0]
	f.outputs = f.outputs[1:]
	return []byte(s), nil
}
func TestGitReadOnlyAndRedactsRemote(t *testing.T) {
	f := &fakeExec{outputs: []string{"abc\n", "feature\n", " M file\n", "https://secret:token@example.com/team/repo?auth=secret#token"}}
	obs, err := (Git{Path: "/tmp/repo with spaces", Exec: f}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs[0].State != "dirty" || obs[0].Revision != "abc" || obs[0].Data["remote"] != "https://example.com/team/repo" {
		t.Fatalf("%+v", obs)
	}
	for _, c := range f.calls {
		if !reflect.DeepEqual(c[:3], []string{"git", "-C", "/tmp/repo with spaces"}) {
			t.Fatal(c)
		}
	}
	if _, err := (Git{Path: "relative", Exec: f}).Collect(context.Background()); err == nil {
		t.Fatal("accepted relative path")
	}
}
func TestGitLabUsesExplicitHostAndCurrentSHA(t *testing.T) {
	f := &fakeExec{outputs: []string{`{"iid":12,"state":"opened","sha":"new","source_branch":"fix","draft":false,"detailed_merge_status":"not_approved","web_url":"https://gitlab.example.com/g/r/-/merge_requests/12","head_pipeline":{"sha":"old","status":"success"}}`}}
	obs, err := (GitLab{Host: "gitlab.example.com", Project: "group/repo", MR: 12, Exec: f}).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if obs[0].Data["pipeline_status"] != "unknown" {
		t.Fatal("stale pipeline promoted")
	}
	want := []string{"glab", "api", "--hostname", "gitlab.example.com", "--method", "GET", "projects/group%2Frepo/merge_requests/12"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Fatalf("%v", f.calls)
	}
	f.outputs = []string{`{"iid":12,"state":"merged","sha":"new","head_pipeline":{"sha":"new","status":"success"}}`}
	obs, err = (GitLab{Host: "gitlab.example.com", Project: "group/repo", MR: 12, Exec: f}).Collect(context.Background())
	if err != nil || obs[0].State != "merged" || obs[0].Data["pipeline_status"] != "success" {
		t.Fatalf("%v %v", obs, err)
	}
}
func TestGitLabRejectsInvalidAndMalformed(t *testing.T) {
	for _, host := range []string{"--help", "https://gitlab.example.com", "host/other", "user@host"} {
		if _, err := (GitLab{Host: host, Project: "g/r", MR: 1}).Collect(context.Background()); err == nil {
			t.Fatal(host)
		}
	}
	f := &fakeExec{outputs: []string{`{"iid":2,"state":"opened","sha":"abc"}`}}
	if _, err := (GitLab{Host: "gitlab.example.com", Project: "g/r", MR: 1, Exec: f}).Collect(context.Background()); err == nil {
		t.Fatal("identity mismatch accepted")
	}
}

type collectFunc func(context.Context) ([]Observation, error)

func (f collectFunc) Collect(ctx context.Context) ([]Observation, error) { return f(ctx) }
func TestRunnerIsolatesFailuresAndPersists(t *testing.T) {
	var saved []Result
	r := Runner{Sources: []Source{{"offline", collectFunc(func(context.Context) ([]Observation, error) { return nil, errors.New("offline") })}, {"healthy", collectFunc(func(context.Context) ([]Observation, error) { return []Observation{{Key: "ok"}}, nil })}}, Sink: func(_ context.Context, result Result) error { saved = append(saved, result); return nil }}
	got, err := r.Once(context.Background())
	if err == nil || len(got) != 2 || len(saved) != 2 || saved[1].Observations[0].Key != "ok" {
		t.Fatalf("%v %v", got, err)
	}
}
func TestWatchCancellationAndSinkFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	r := Runner{Sources: []Source{{"s", collectFunc(func(context.Context) ([]Observation, error) { n++; return nil, nil })}}, Sink: func(context.Context, Result) error { cancel(); return nil }}
	if err := r.Watch(ctx, time.Hour); !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	r.Sink = func(context.Context, Result) error { return errors.New("disk full") }
	if err := r.Watch(context.Background(), time.Hour); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatal(err)
	}
}
func TestCommandOutputBounded(t *testing.T) {
	var b limitedBuffer
	if _, err := b.Write(make([]byte, (4<<20)+1)); err == nil {
		t.Fatal("unbounded")
	}
}

func TestRunnerTimeoutDoesNotBlockNextSource(t *testing.T) {
	r := Runner{Timeout: time.Millisecond, Sources: []Source{
		{"slow", collectFunc(func(ctx context.Context) ([]Observation, error) { <-ctx.Done(); return nil, ctx.Err() })},
		{"fast", collectFunc(func(context.Context) ([]Observation, error) { return []Observation{{Key: "alive"}}, nil })},
	}}
	got, err := r.Once(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) || len(got) != 2 || got[1].Observations[0].Key != "alive" {
		t.Fatalf("%v %v", got, err)
	}
}
func TestWatchRetriesCollectionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	r := Runner{Sources: []Source{{"s", collectFunc(func(context.Context) ([]Observation, error) { return nil, errors.New("temporarily offline") })}}, Sink: func(_ context.Context, r Result) error {
		if r.Error == "" {
			t.Fatal("failure not recorded")
		}
		n++
		if n == 2 {
			cancel()
		}
		return nil
	}}
	if err := r.Watch(ctx, time.Millisecond); !errors.Is(err, context.Canceled) || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
}
