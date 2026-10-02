package tailnet

import "testing"

func TestParse(t *testing.T) {
	st, err := Parse([]byte(`{"BackendState":"Running",
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
