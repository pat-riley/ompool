package instrument

import (
	"net"
	"strings"
	"testing"
)

func TestEmbeddedScriptsArePresent(t *testing.T) {
	if !strings.Contains(Source, "/btc/tx") {
		t.Fatal("embedded instrument does not listen for /btc/tx")
	}
	if !strings.Contains(Source, "instrument ready") {
		t.Fatal("embedded instrument never prints the ready line the launcher waits for")
	}
	if !strings.Contains(TestSender, "/btc/block") {
		t.Fatal("embedded test sender does not send a block")
	}
}

func TestOSCPort(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:57120": "57120",
		"localhost:1":     "1",
		"nonsense":        "",
		"":                "",
	}
	for addr, want := range cases {
		if got := oscPort(addr); got != want {
			t.Errorf("oscPort(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestPortInUseDetectsListener(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot open UDP socket:", err)
	}
	defer conn.Close()
	if !portInUse(conn.LocalAddr().String()) {
		t.Fatal("expected a held port to read as in use")
	}
	if portInUse("127.0.0.1:0") {
		t.Fatal("port 0 must never read as in use")
	}
}

func TestStartRespectsOff(t *testing.T) {
	t.Setenv(Env, "off")
	p := Start("127.0.0.1:57120")
	if state, _ := p.Status(); state != Off {
		t.Fatalf("state = %v, want Off", state)
	}
	p.Stop() // must be safe on a process that never started
}

func TestStartRejectsMissingScript(t *testing.T) {
	t.Setenv(Env, t.TempDir()+"/nope.scd")
	t.Setenv("PATH", t.TempDir()) // hide sclang so the missing state wins first
	p := Start("127.0.0.1:0")
	if state, _ := p.Status(); state != Missing {
		t.Fatalf("state = %v, want Missing when sclang is absent", state)
	}
}
