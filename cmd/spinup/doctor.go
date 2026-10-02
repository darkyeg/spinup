package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkyeg/spinup/internal/agentconfig"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/doctor"
	"github.com/darkyeg/spinup/internal/proxy"
	"github.com/darkyeg/spinup/internal/release"
	"github.com/darkyeg/spinup/internal/shell"
	"github.com/darkyeg/spinup/internal/source"
	"github.com/darkyeg/spinup/internal/tailnet"
	"github.com/darkyeg/spinup/internal/tools"
)

type doctorCmd struct{}

func (doctorCmd) Help() string {
	return `Checks this machine: tools, Tailscale and the paths to your other machines, the accounts,
skills and agent config, and whether the repos and spinup itself are up to date. Every problem
comes with the command that fixes it.`
}

func (doctorCmd) Run() error {
	ctx := context.Background()
	repo := repoData()
	sections := []doctor.Section{
		toolsSection(repo),
		networkSection(ctx),
		accountsSection(ctx),
		agentsSection(repo),
		doctor.Repos(checkouts(ctx, repo)),
		doctor.Update(version, latestVersion(ctx, releases)),
	}
	printSections(sections)
	if n := doctor.Failures(sections); n > 0 {
		return fmt.Errorf("%d problem(s); run the fix commands above", n)
	}
	fmt.Println("\nAll good.")
	return nil
}

func printSections(sections []doctor.Section) {
	marks := map[doctor.Level]string{doctor.OK: "[ok]", doctor.Warn: "[!!]", doctor.Fail: "[XX]"}
	for i, s := range sections {
		if len(s.Checks) == 0 {
			continue
		}
		if i > 0 {
			fmt.Println()
		}
		fmt.Println(s.Title)
		for _, c := range s.Checks {
			fmt.Printf("  %s %s\n", marks[c.Level], c.Label)
			if c.Fix != "" {
				fmt.Printf("       fix: %s\n", c.Fix)
			}
		}
	}
}

func toolsSection(repo source.Source) doctor.Section {
	all, err := tools.Load(repo)
	if err != nil {
		return doctor.Section{Title: "Tools", Checks: []doctor.Check{{Level: doctor.Fail, Label: err.Error()}}}
	}
	return doctor.Tools(toolNames(tools.Missing(all)), len(all))
}

func networkSection(ctx context.Context) doctor.Section {
	cli := tailnet.CLI{}
	st, _ := cli.Status(ctx)
	paths := map[string]tailnet.Path{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range st.Peers {
		if !p.Online {
			continue
		}
		wg.Go(func() {
			if path, err := cli.PathTo(ctx, p.Name); err == nil {
				mu.Lock()
				paths[p.Name] = path
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return doctor.Network(st, paths, time.Now())
}

func accountsSection(ctx context.Context) doctor.Section {
	cfg, err := config.Load()
	if errors.Is(err, config.ErrNotInstalled) {
		return doctor.Accounts(doctor.Service{}, cfg.Port)
	}
	svc := doctor.Service{Installed: true}
	if r, err := localService(cfg, "").report(); err == nil {
		svc.Report = &r
	} else {
		svc.Problem = err.Error()
	}
	if cfg.Hold.CanHold() {
		svc.ProxyVersion = proxy.InstalledVersion(cfg)
		svc.LoginOnly = runtime.GOOS == "darwin"
		svc.ProxyLatest = latestVersion(ctx, proxy.Project)
	}
	return doctor.Accounts(svc, cfg.Port)
}

func agentsSection(repo source.Source) doctor.Section {
	manager := skillManager(repo)
	list, err := manager.List()
	if err != nil {
		return doctor.Section{Title: "Agents", Checks: []doctor.Check{{Level: doctor.Fail, Label: "skills: " + err.Error()}}}
	}
	var notInstalled []string
	for _, s := range list {
		if !s.Installed {
			notInstalled = append(notInstalled, s.Name)
		}
	}
	unlisted, err := manager.Unlisted()
	if err != nil {
		return doctor.Section{Title: "Agents", Checks: []doctor.Check{{Level: doctor.Fail, Label: "skills: " + err.Error()}}}
	}
	drifted, err := agentconfig.Drifted(repo, agentconfig.HostHomes())
	if err != nil {
		drifted = []string{err.Error()}
	}
	return doctor.Agents(notInstalled, unlisted, drifted, len(list))
}

func checkouts(ctx context.Context, repo source.Source) []doctor.Checkout {
	dir, err := repo.Checkout()
	if err != nil {
		return nil
	}
	dirs := map[string]string{"spinup checkout": dir}
	if private, ok := repo.PrivatePath(); ok {
		dirs["private repo"] = private
	}
	var out []doctor.Checkout
	for name, d := range dirs {
		if c, ok := checkoutState(ctx, name, d); ok {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b doctor.Checkout) int { return strings.Compare(b.Name, a.Name) })
	return out
}

func checkoutState(ctx context.Context, name, dir string) (doctor.Checkout, bool) {
	git := func(args ...string) (string, error) {
		return shell.Output(ctx, "git", append([]string{"-C", dir}, args...)...)
	}
	_, _ = git("fetch", "-q")
	status, err := git("status", "--porcelain")
	if err != nil {
		return doctor.Checkout{}, false
	}
	c := doctor.Checkout{Name: name, Dirty: strings.TrimSpace(status) != ""}
	if counts, err := git("rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
		if f := strings.Fields(counts); len(f) == 2 {
			c.Ahead, _ = strconv.Atoi(f[0])
			c.Behind, _ = strconv.Atoi(f[1])
		}
	}
	return c, true
}

// latestVersion is the newest release of a GitHub project, or "" when it can't be read.
func latestVersion(ctx context.Context, repo string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rel, err := release.Latest(ctx, repo)
	if err != nil {
		return ""
	}
	return rel.Version
}
