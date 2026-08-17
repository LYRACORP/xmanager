package docker

import "testing"

func TestParseJoinCommand(t *testing.T) {
	out := `To add a worker to this swarm, run the following command:

    docker swarm join \
    --token SWMTKN-1-49nj1cmql0jkz5s954yi3oex3nedyz0fb0xx14ie39trti4wxv-8vxv8rssmk743ojnwacrr2e7c \
    192.168.99.100:2377

`
	got := ParseJoinCommand(out)
	want := "docker swarm join --token SWMTKN-1-49nj1cmql0jkz5s954yi3oex3nedyz0fb0xx14ie39trti4wxv-8vxv8rssmk743ojnwacrr2e7c 192.168.99.100:2377"
	if got != want {
		t.Fatalf("got %q", got)
	}
	one := ParseJoinCommand("docker swarm join --token SWMTKN-1-abc-def 10.0.0.1:2377\n")
	if !stringsHasPrefix(one, "docker swarm join --token") {
		t.Fatalf("one-line: %q", one)
	}
	if ParseJoinCommand("nothing useful") != "" {
		t.Fatal("expected empty")
	}
}

func stringsHasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func TestParseSwarmInfo(t *testing.T) {
	info := ParseSwarmInfo("active\ttrue\tabc123\tnode1\t10.0.0.5")
	if !info.Active() || !info.Manager() || info.AdvertiseAddr != "10.0.0.5" {
		t.Fatalf("%+v", info)
	}
	idle := ParseSwarmInfo("inactive\tfalse\t\t\t")
	if idle.Active() || idle.Manager() {
		t.Fatalf("%+v", idle)
	}
}

func TestParseNodeLS(t *testing.T) {
	out := "x1\tmanager-1\tReady\tActive\tLeader\nx2\tworker-1\tReady\tActive\t\n"
	nodes := ParseNodeLS(out)
	if len(nodes) != 2 {
		t.Fatalf("len=%d", len(nodes))
	}
	if !nodes[0].IsManager() || nodes[0].ManagerStatus != "Leader" {
		t.Fatalf("%+v", nodes[0])
	}
	if nodes[1].IsManager() {
		t.Fatalf("worker should not be manager: %+v", nodes[1])
	}
}

func TestParseServiceLS(t *testing.T) {
	out := "abc\tweb\treplicated\t2/2\tnginx:latest\n"
	svcs := ParseServiceLS(out)
	if len(svcs) != 1 || svcs[0].Name != "web" || svcs[0].Replicas != "2/2" {
		t.Fatalf("%+v", svcs)
	}
}

func TestFirstIPv4(t *testing.T) {
	if got := FirstIPv4("127.0.0.1 10.0.0.8 172.17.0.1"); got != "10.0.0.8" {
		t.Fatalf("got %q", got)
	}
	if FirstIPv4("::1") != "" {
		t.Fatal("expected skip ipv6-only")
	}
}

func TestParseJoinParts(t *testing.T) {
	cmd := "docker swarm join --token SWMTKN-1-aaaa-bbbb 203.0.113.50:2377"
	tok, addr, ok := ParseJoinParts(cmd)
	if !ok || tok != "SWMTKN-1-aaaa-bbbb" || addr != "203.0.113.50:2377" {
		t.Fatalf("%q %q %v", tok, addr, ok)
	}
	if JoinListenAddr("10.0.0.5", "") != "10.0.0.5:2377" {
		t.Fatal(JoinListenAddr("10.0.0.5", ""))
	}
	if JoinListenAddr("10.0.0.5:2377", "") != "10.0.0.5:2377" {
		t.Fatal("already has port")
	}
}

func TestSanitizeJoin(t *testing.T) {
	tok, err := sanitizeToken("SWMTKN-1-aaaa-bbbb")
	if err != nil || tok == "" {
		t.Fatal(err)
	}
	if _, err := sanitizeToken("not-a-token"); err == nil {
		t.Fatal("expected reject token")
	}
	addr, err := sanitizeJoinAddr("10.0.0.5:2377")
	if err != nil || addr != "10.0.0.5:2377" {
		t.Fatalf("%q %v", addr, err)
	}
	addr, err = sanitizeJoinAddr("10.0.0.5")
	if err != nil || addr != "10.0.0.5:2377" {
		t.Fatalf("bare ip %q %v", addr, err)
	}
	if _, err := sanitizeJoinAddr("127.0.0.1:2377"); err == nil {
		t.Fatal("expected reject loopback")
	}
	if _, err := sanitizeAdvertiseIP("127.0.0.1"); err == nil {
		t.Fatal("expected reject loopback advertise")
	}
	if _, err := sanitizeNodeID("abc;rm"); err == nil {
		t.Fatal("expected reject node id")
	}
	if nextAvailability("active") != "drain" || nextAvailability("drain") != "pause" {
		t.Fatal("availability cycle")
	}
}
