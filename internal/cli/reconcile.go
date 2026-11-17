package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/beeemT/blackboard/internal/domain"
	"github.com/beeemT/blackboard/internal/reconcile"
	"github.com/beeemT/blackboard/internal/service"
	"io"
	"os"
	"path/filepath"
	"time"
)

type config struct {
	Sources []sourceConfig `json:"sources"`
}
type sourceConfig struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Host    string `json:"host,omitempty"`
	Project string `json:"project,omitempty"`
	MR      int    `json:"mr,omitempty"`
}

func runReconcile(ctx context.Context, s *service.Service, args []string, out, errout io.Writer) int {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("config", "", "explicit sources JSON file")
	watch := fs.Bool("watch", false, "repeat until canceled")
	interval := fs.Duration("interval", 5*time.Minute, "delay between runs")
	if e := fs.Parse(args); e != nil {
		return fail(errout, e)
	}
	if *path == "" || len(fs.Args()) > 0 {
		return fail(errout, errors.New("invalid: --config required"))
	}
	f, e := os.Open(*path)
	if e != nil {
		return fail(errout, e)
	}
	defer f.Close()
	var cfg config
	d := json.NewDecoder(io.LimitReader(f, 1024*1024))
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return fail(errout, e)
	}
	if len(cfg.Sources) == 0 {
		return fail(errout, errors.New("invalid: sources cannot be empty"))
	}
	targets := map[string]string{}
	r := reconcile.Runner{}
	for _, c := range cfg.Sources {
		var collector reconcile.Collector
		switch c.Kind {
		case "git":
			collector = reconcile.Git{Path: c.Path}
			targets[c.Name] = filepath.Clean(c.Path)
		case "gitlab":
			collector = reconcile.GitLab{Host: c.Host, Project: c.Project, MR: c.MR}
			targets[c.Name] = fmt.Sprintf("https://%s/%s/-/merge_requests/%d", c.Host, c.Project, c.MR)
		default:
			return fail(errout, errors.New("invalid: source kind must be git or gitlab"))
		}
		r.Sources = append(r.Sources, reconcile.Source{Name: c.Name, Collector: collector})
	}
	r.Sink = func(ctx context.Context, result reconcile.Result) error {
		obs := []domain.Observation{}
		state := "ok"
		if result.Error != "" {
			state = "error"
		} else {
			for _, o := range result.Observations {
				obs = append(obs, domain.Observation{Source: o.Source, Key: o.Key, State: o.State, Revision: o.Revision, Data: o.Data, ObservedAt: o.ObservedAt})
			}
		}
		// Failure health is separate from the last good observation; do not erase known state.
		obs = append(obs, domain.Observation{Source: "collector", Key: result.Source, State: state, ObservedAt: result.CheckedAt, Data: map[string]string{"message": result.Error, "target": targets[result.Source]}})
		b := make([]byte, 16)
		if _, e := rand.Read(b); e != nil {
			return e
		}
		_, e := s.Execute(ctx, service.Command{Operation: "observe", OperationID: "reconcile-" + hex.EncodeToString(b), Actor: "reconciler", Observations: obs})
		if e != nil {
			return e
		}
		if emit(out, result) != 0 {
			return errors.New("output failed")
		}
		return nil
	}
	if *watch {
		e = r.Watch(ctx, *interval)
		if errors.Is(e, context.Canceled) {
			return 0
		}
		if e != nil {
			return fail(errout, e)
		}
		return 0
	}
	_, e = r.Once(ctx)
	if e != nil {
		return fail(errout, e)
	}
	return 0
}
