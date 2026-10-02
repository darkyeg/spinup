package tailnet

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	st, err := parse([]byte(`{"BackendState":"Running",
		"Self":{"HostName":"DESKTOP-1","DNSName":"office-pc.tail1234.ts.net.","OS":"windows","TailscaleIPs":["100.64.0.1","fd7a::1"],"Online":true},
		"Peer":{"k":{"HostName":"laptop","DNSName":"Laptop.tail1234.ts.net.","OS":"linux","TailscaleIPs":["fd7a::2","100.64.0.2"],"Online":false,"LastSeen":"2026-10-01T10:00:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Self.Name != "office-pc" || st.Self.IP != "100.64.0.1" || !st.Self.Online {
		t.Errorf("self = %+v", st.Self)
	}
	p, ok := st.Peer("laptop")
	if !ok || p.IP != "100.64.0.2" || p.Online || p.LastSeen.IsZero() {
		t.Errorf("peer = %+v", p)
	}
}

func TestParseReadsWhatDoctorChecks(t *testing.T) {
	st, err := parse([]byte(`{"BackendState":"Running","CertDomains":["a.ts.net"],"CurrentTailnet":{"MagicDNSEnabled":true},
		"Self":{"DNSName":"pc.tail1.ts.net.","TailscaleIPs":["100.64.0.1"],"Online":true,"KeyExpiry":"2027-01-02T03:04:05Z"},
		"Peer":{"k":{"DNSName":"mac.tail1.ts.net.","TailscaleIPs":["100.64.0.2"],"Expired":true,"KeyExpiry":"2026-01-01T00:00:00Z"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.BackendState != "Running" || len(st.CertDomains) != 1 || !st.MagicDNS {
		t.Errorf("status = %+v", st)
	}
	if !st.Self.IsSelf || st.Self.DNSName != "pc.tail1.ts.net" || st.Self.KeyExpiry.Year() != 2027 || st.Self.Expired {
		t.Errorf("self = %+v", st.Self)
	}
	if mac, _ := st.Peer("mac"); mac.IsSelf || !mac.Expired {
		t.Errorf("peer = %+v", mac)
	}
}

func TestMachinesListThisOneFirstThenByName(t *testing.T) {
	st := Status{
		Self:  Node{Name: "zed", IP: "100.64.0.9", IsSelf: true},
		Peers: []Node{{Name: "mac", IP: "100.64.0.2"}, {Name: "air", IP: "100.64.0.3"}},
	}
	var names []string
	for _, n := range st.Machines() {
		names = append(names, n.Name)
	}
	if got := strings.Join(names, ","); got != "zed,air,mac" {
		t.Errorf("order = %s", got)
	}
}

func TestValidName(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"pc", true}, {"office-pc", true}, {"a", true}, {"mac2", true}, {strings.Repeat("a", 63), true},
		{"", false}, {"-pc", false}, {"pc-", false}, {"Office", false}, {"my_pc", false},
		{"my pc", false}, {"pc.local", false}, {strings.Repeat("a", 64), false},
	}
	for _, tc := range tests {
		if got := ValidName(tc.name); got != tc.valid {
			t.Errorf("ValidName(%q) = %v", tc.name, got)
		}
	}
}

func TestUpArgs(t *testing.T) {
	tests := []struct {
		name    string
		windows bool
		want    string
	}{
		{"", false, "up"},
		{"pc", false, "up --hostname=pc"},
		{"pc", true, "up --unattended --hostname=pc"},
		{"", true, "up --unattended"},
	}
	for _, tc := range tests {
		if got := strings.Join(upArgs(tc.name, tc.windows), " "); got != tc.want {
			t.Errorf("upArgs(%q, %v) = %q", tc.name, tc.windows, got)
		}
	}
}

func TestParsePing(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   Path
	}{
		{"no answer", "ping \"mac\" timed out\nping \"mac\" timed out\n", Path{}},
		{"empty", "", Path{}},
		{"relay", "pong from mac (100.64.0.2) via DERP(fra) in 31ms\npong from mac (100.64.0.2) via DERP(fra) in 29ms\n",
			Path{Relay, "DERP(fra)"}},
		{"lan 192.168", "pong from mac (100.64.0.2) via 192.168.1.20:41641 in 3ms\n", Path{LAN, "192.168.1.20:41641"}},
		{"lan 10", "pong from mac (100.64.0.2) via 10.0.0.5:41641 in 3ms\n", Path{LAN, "10.0.0.5:41641"}},
		{"lan 172.16", "pong from mac (100.64.0.2) via 172.16.0.5:41641 in 3ms\n", Path{LAN, "172.16.0.5:41641"}},
		{"lan 172.31", "pong from mac (100.64.0.2) via 172.31.255.5:41641 in 3ms\n", Path{LAN, "172.31.255.5:41641"}},
		{"not lan 172.32", "pong from mac (100.64.0.2) via 172.32.0.5:41641 in 3ms\n", Path{Direct, "172.32.0.5:41641"}},
		{"not lan 172.15", "pong from mac (100.64.0.2) via 172.15.0.5:41641 in 3ms\n", Path{Direct, "172.15.0.5:41641"}},
		{"public", "pong from mac (100.64.0.2) via 203.0.113.7:41641 in 20ms\n", Path{Direct, "203.0.113.7:41641"}},
		{"relay then direct uses the last", "pong from mac (100.64.0.2) via DERP(fra) in 31ms\npong from mac (100.64.0.2) via 203.0.113.7:41641 in 9ms\n",
			Path{Direct, "203.0.113.7:41641"}},
		{"ipv6 public", "pong from mac (100.64.0.2) via [2001:db8::1]:41641 in 9ms\n", Path{Direct, "[2001:db8::1]:41641"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePing(tc.output); got != tc.want {
				t.Errorf("parsePing = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHostnameNoteSaysWhenTheNameDidNotTake(t *testing.T) {
	if got := hostnameNote("office-pc", "office-pc"); got != "" {
		t.Errorf("same name: %q", got)
	}
	if got := hostnameNote("", "anything"); got != "" {
		t.Errorf("no name asked for: %q", got)
	}
	if got := hostnameNote("office-pc", "office-pc-1"); !strings.Contains(got, "office-pc-1") {
		t.Errorf("renamed: %q", got)
	}
}

func TestWingetNothingToUpgradeIsNotAFailure(t *testing.T) {
	for code, want := range map[int]bool{0: false, 1: false, 0x8A15002B: true, -1978335189: true} {
		if got := wingetNothingToUpgrade(code); got != want {
			t.Errorf("exit %d: got %v", code, got)
		}
	}
}
