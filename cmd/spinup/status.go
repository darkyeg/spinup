package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/darkyeg/spinup/internal/api"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/leadership"
)

type statusCmd struct {
	JSON bool `help:"Print the machine's report as JSON."`
}

func (c statusCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	r, err := localService(cfg, "").report()
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	printReport(os.Stdout, r, cfg.Port)
	return nil
}

func printReport(out io.Writer, r api.Report, port int) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "This machine\t%s, %s (spinup %s)\n", r.Name, holdPhrase(r.Hold), r.Version)
	fmt.Fprintf(w, "Accounts\t%s\n", accountsLine(r))
	if r.Waiting != "" {
		fmt.Fprintf(w, "Waiting\t%s\n", r.Waiting)
	}
	fmt.Fprintf(w, "Address\thttp://localhost:%d (dashboard: /management.html)\n", port)
	w.Flush()

	if len(r.Accounts) > 0 {
		fmt.Fprintf(out, "\nLogins\n")
		for _, a := range r.Accounts {
			fmt.Fprintf(w, "  %s\t%s\trefreshed %s\n", a.Type, a.Name, ago(a.Refreshed))
		}
		w.Flush()
	}
	if len(r.Peers) > 0 {
		fmt.Fprintf(out, "\nOther machines that can hold the accounts\n")
		for _, p := range r.Peers {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", p.Name, p.Hold, peerLine(p))
		}
		w.Flush()
	}
}

func accountsLine(r api.Report) string {
	switch {
	case r.Leading && r.ProxyRunning:
		return fmt.Sprintf("held here (epoch %d)%s", r.Epoch, runningRequests(r.InFlight))
	case r.Leading:
		return fmt.Sprintf("held here (epoch %d), but CLIProxyAPI isn't running", r.Epoch)
	case !r.Hold.CanHold() && r.LeaderAddr != "":
		return "used through " + r.LeaderAddr
	case r.Leader != "" && r.Waiting == "":
		return fmt.Sprintf("held by %s (epoch %d); %s", r.Leader, r.Epoch, syncLine(r.Synced))
	}
	return "nobody holds them right now"
}

func runningRequests(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return ", 1 request running"
	}
	return fmt.Sprintf(", %d requests running", n)
}

func syncLine(s *api.Sync) string {
	if s == nil {
		return "the copy here isn't synced yet"
	}
	return fmt.Sprintf("the copy here synced %s ago", time.Duration(s.SecondsAgo)*time.Second)
}

func peerLine(p leadership.Peer) string {
	switch p.State {
	case leadership.Leading:
		return "holds the accounts"
	case leadership.Synced:
		return "standing by, synced"
	case leadership.Standing:
		return "standing by"
	case leadership.Silent:
		return "online, but spinup doesn't answer"
	}
	return "offline for " + p.OfflineFor.Round(time.Second).String()
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "at an unknown time"
	}
	return time.Since(t).Round(time.Minute).String() + " ago"
}
