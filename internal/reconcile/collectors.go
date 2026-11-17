package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Executor interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// CommandExecutor never invokes a shell. Output is bounded and stderr is not
// returned because external tools can include credentials in diagnostics.
type CommandExecutor struct{}

func (CommandExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	var b limitedBuffer
	c.Stdout = &b
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w", name, err)
	}
	return b.Bytes(), nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4<<20 {
		return 0, errors.New("command output exceeds 4 MiB")
	}
	return b.Buffer.Write(p)
}
func executor(e Executor) Executor {
	if e != nil {
		return e
	}
	return CommandExecutor{}
}

type Git struct {
	Path string
	Exec Executor
	Now  func() time.Time
}

func (g Git) Collect(ctx context.Context) ([]Observation, error) {
	if !filepath.IsAbs(g.Path) {
		return nil, errors.New("worktree path must be absolute")
	}
	e := executor(g.Exec)
	read := func(args ...string) (string, error) {
		out, err := e.Run(ctx, "git", append([]string{"-C", g.Path}, args...)...)
		return strings.TrimSpace(string(out)), err
	}
	head, err := read("rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, err
	}
	branch, err := read("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	status, err := read("status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	remote, err := read("config", "--get", "remote.origin.url")
	if err != nil {
		remote = ""
	} // Local-only repositories are valid.
	state := "clean"
	if status != "" {
		state = "dirty"
	}
	return []Observation{{Source: "git", Kind: "worktree", Key: filepath.Clean(g.Path), State: state, Revision: head, ObservedAt: now(g.Now), Data: map[string]string{"branch": branch, "remote": sanitizeRemote(remote)}}}, nil
}
func sanitizeRemote(raw string) string {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	// SCP form: discard user portion (which can contain credentials).
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

// GitLab reads one explicitly linked MR using the user's existing glab login.
// It never fetches unrelated projects, executes git hooks, or modifies the MR.
type GitLab struct {
	Host, Project string
	MR            int
	Exec          Executor
	Now           func() time.Time
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]+)?$`)

func (g GitLab) Collect(ctx context.Context) ([]Observation, error) {
	if !hostPattern.MatchString(g.Host) || strings.Contains(g.Host, "..") {
		return nil, errors.New("invalid GitLab hostname")
	}
	if g.Project == "" || strings.HasPrefix(g.Project, "/") || strings.HasSuffix(g.Project, "/") || strings.ContainsAny(g.Project, "?#\\\x00\n\r") || g.MR <= 0 {
		return nil, errors.New("project path and positive MR number required")
	}
	for _, p := range strings.Split(g.Project, "/") {
		if p == "" || p == "." || p == ".." {
			return nil, errors.New("invalid project path")
		}
	}
	endpoint := "projects/" + url.PathEscape(g.Project) + "/merge_requests/" + strconv.Itoa(g.MR)
	out, err := executor(g.Exec).Run(ctx, "glab", "api", "--hostname", g.Host, "--method", "GET", endpoint)
	if err != nil {
		return nil, err
	}
	var mr struct {
		IID          int    `json:"iid"`
		State        string `json:"state"`
		SHA          string `json:"sha"`
		WebURL       string `json:"web_url"`
		SourceBranch string `json:"source_branch"`
		Detailed     string `json:"detailed_merge_status"`
		Draft        bool   `json:"draft"`
		Pipeline     *struct {
			Status string `json:"status"`
			SHA    string `json:"sha"`
		} `json:"head_pipeline"`
	}
	if err = json.Unmarshal(out, &mr); err != nil {
		return nil, errors.New("invalid GitLab MR JSON")
	}
	if mr.IID != g.MR || mr.State == "" || mr.SHA == "" {
		return nil, errors.New("GitLab response missing matching MR identity, state or SHA")
	}
	data := map[string]string{"branch": mr.SourceBranch, "merge_status": mr.Detailed, "draft": strconv.FormatBool(mr.Draft)}
	// Only accept the observed URL if it stays on the explicitly authorized host.
	if u, e := url.Parse(mr.WebURL); e == nil && u.Scheme == "https" && u.Host == g.Host && u.User == nil {
		data["url"] = u.String()
	}
	data["pipeline_status"] = "unknown"
	if mr.Pipeline != nil {
		data["pipeline_sha"] = mr.Pipeline.SHA
		if mr.Pipeline.SHA == mr.SHA {
			data["pipeline_status"] = mr.Pipeline.Status
		}
	}
	return []Observation{{Source: "gitlab", Kind: "merge_request", Key: "https://" + g.Host + "/" + g.Project + "/-/merge_requests/" + strconv.Itoa(g.MR), State: mr.State, Revision: mr.SHA, Data: data, ObservedAt: now(g.Now)}}, nil
}
