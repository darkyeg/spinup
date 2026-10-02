package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/tailnet"
)

type setupCmd struct {
	Name     string `arg:"" optional:"" help:"This machine's name on your tailnet; other machines reach it by it (asked when omitted)."`
	Hub      bool   `xor:"hold" help:"This machine normally holds the accounts. One hub per tailnet."`
	Standby  bool   `xor:"hold" help:"This machine takes the accounts while the hub is off."`
	Password string `env:"SPINUP_PASSWORD" placeholder:"PASSWORD" help:"The dashboard password, for --standby (asked when omitted; on the hub: spinup keys)."`
	APIKey   string `name:"api-key" env:"SPINUP_API_KEY" placeholder:"KEY" help:"The API key, for a machine that only uses the accounts (asked when omitted; on the hub: spinup keys)."`
}

func (setupCmd) Help() string {
	return `Sets up the whole machine: Tailscale, the accounts service, dev tools, skills and agent
config, then checks it all. Without --hub or --standby the machine only uses the accounts.
Re-running setup keeps the machine's hold and repairs everything else.`
}

// setupStep is one part of setup; a needed step stops setup when it fails, the others are reported at the end.
type setupStep struct {
	title  string
	needed bool
	run    func(context.Context) error
}

func (c setupCmd) Run() error {
	ctx := context.Background()
	repo := repoData()
	name, err := c.machineName(ctx)
	if err != nil {
		return err
	}
	steps := []setupStep{
		{"Tailscale", true, func(ctx context.Context) error { return tailnet.Setup(ctx, name) }},
		{"Accounts service", true, func(ctx context.Context) error {
			return installService(ctx, serviceRequest{hold: c.hold(), password: c.Password, apiKey: c.APIKey, repo: repo})
		}},
		{"Dev tools", false, func(ctx context.Context) error { return installTools(ctx, repo) }},
		{"Skills", false, func(ctx context.Context) error { return syncSkills(ctx, repo) }},
		{"Agent config", false, func(context.Context) error { return installAgentConfig(repo) }},
	}
	var failed []string
	for i, s := range steps {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(steps), s.title)
		if err := s.run(ctx); err != nil {
			if s.needed {
				return fmt.Errorf("%s: %w", s.title, err)
			}
			fmt.Printf("    %s failed: %v\n", s.title, err)
			failed = append(failed, s.title)
		}
	}
	fmt.Println()
	printAddresses(ctx, name)
	fmt.Println()
	if err := (doctorCmd{}).Run(); err != nil {
		return err
	}
	if len(failed) > 0 {
		return fmt.Errorf("these steps failed: %s (re-run spinup setup after fixing them)", strings.Join(failed, ", "))
	}
	return nil
}

func (c setupCmd) hold() config.Hold {
	switch {
	case c.Hub:
		return config.HoldHub
	case c.Standby:
		return config.HoldStandby
	}
	return ""
}

func (c setupCmd) machineName(ctx context.Context) (string, error) {
	current := currentMachineName(ctx)
	name := strings.ToLower(strings.TrimSpace(c.Name))
	if name == "" && interactive() {
		fmt.Println("This machine's name is its address on your tailnet: other machines reach it as <name>\n" +
			"(http://<name>:8317, ssh <name>). Lowercase letters, digits and dashes.")
		name = strings.ToLower(askLine(fmt.Sprintf("Name for this machine [%s]: ", current)))
	}
	if name == "" {
		name = current
	}
	if !tailnet.ValidName(name) {
		return "", fmt.Errorf("%q can't be a machine name: use lowercase letters, digits and dashes (e.g. office-pc)", name)
	}
	return name, nil
}

var notNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// currentMachineName is the Tailscale name when there is one, else the OS hostname made valid.
func currentMachineName(ctx context.Context) string {
	if st, err := (tailnet.CLI{}).Status(ctx); err == nil && st.Self.Name != "" {
		return st.Self.Name
	}
	hostname, _ := os.Hostname()
	return strings.Trim(notNameChars.ReplaceAllString(strings.ToLower(hostname), "-"), "-")
}

func printAddresses(ctx context.Context, name string) {
	if st, err := (tailnet.CLI{}).Status(ctx); err == nil && st.Self.Name != "" {
		name = st.Self.Name
	}
	fmt.Printf("This machine is %s on your tailnet. From your other machines:\n", name)
	fmt.Printf("  any service:  http://%s:<port>\n", name)
	fmt.Printf("  ssh:          ssh <user>@%s\n", name)
	fmt.Println("  all machines: spinup machines")
}
