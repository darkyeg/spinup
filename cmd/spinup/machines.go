package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/darkyeg/spinup/internal/tailnet"
)

type machinesCmd struct{}

func (machinesCmd) Help() string {
	return `Lists every device on your tailnet, straight from Tailscale, so it is never out of date.
Reach any service on a device at http://<name>:<port>.`
}

func (machinesCmd) Run() error {
	st, err := (tailnet.CLI{}).Status(context.Background())
	if err != nil {
		return fmt.Errorf("can't read Tailscale's status: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, n := range st.Machines() {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.Name, n.IP, n.DNSName, machineState(n))
	}
	w.Flush()
	fmt.Println("\nIf a short name doesn't resolve, use the full name or the IP.")
	return nil
}

func machineState(n tailnet.Node) string {
	state := n.OS + ", offline"
	if n.Online {
		state = n.OS + ", online"
	}
	if n.IsSelf {
		state += ", this machine"
	}
	return state
}
