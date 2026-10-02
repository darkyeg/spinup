package tailnet

import "sort"

// Machines is every machine on the tailnet: this one first, then the others by name.
func (s Status) Machines() []Node {
	machines := make([]Node, 0, 1+len(s.Peers))
	if s.Self.IP != "" {
		machines = append(machines, s.Self)
	}
	others := append([]Node(nil), s.Peers...)
	sort.Slice(others, func(i, j int) bool { return others[i].Name < others[j].Name })
	return append(machines, others...)
}
