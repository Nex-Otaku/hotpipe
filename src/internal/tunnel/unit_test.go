package tunnel

import (
	"bufio"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

func TestArgs(t *testing.T) {
	tn := config.Tunnel{ID: "x", Host: "203.0.113.10", Port: 2222, User: "deploy",
		LocalPort: 5001, RemotePort: 3306}
	tn.Normalize()
	a := Args(tn, config.Settings{ServerAliveInterval: 5, ServerAliveCountMax: 3})
	s := strings.Join(a, " ")
	for _, want := range []string{"-N", "BatchMode=yes", "ExitOnForwardFailure=yes",
		"ServerAliveInterval=5", "ServerAliveCountMax=3", "-p 2222",
		"-L localhost:5001:127.0.0.1:3306", "-l deploy", "-- 203.0.113.10"} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q в %s", want, s)
		}
	}
	if slices.Contains(a, "-f") {
		t.Error("-f увёл бы ssh из-под надзора")
	}
	if a[len(a)-1] != tn.Host || a[len(a)-2] != "--" {
		t.Error("хост должен идти последним, после --")
	}

	tn.LocalHost, tn.RemoteHost, tn.IdentityFile = "::1", "fe80::1", "/k"
	s = strings.Join(Args(tn, config.Settings{ServerAliveInterval: 1, ServerAliveCountMax: 1}), " ")
	if !strings.Contains(s, "-L [::1]:5001:[fe80::1]:3306") || !strings.Contains(s, "-i /k") {
		t.Errorf("IPv6 или ключ: %s", s)
	}
}

const tcpTable = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1389 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111 1 0000000000000000 100 0 0 10 0
   1: 0100007F:1389 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1000        0 222 1 0000000000000000 100 0 0 10 0
   2: 0100007F:B4DE 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 333 1 0000000000000000 100 0 0 10 0
`

func TestParseListeners(t *testing.T) {
	out := map[uint64]bool{}
	parseListeners(bufio.NewScanner(strings.NewReader(tcpTable)), 5001, out)
	if len(out) != 1 || !out[111] {
		t.Fatalf("ожидался только слушающий 111: %v", out)
	}
}

func TestDiagnose(t *testing.T) {
	cases := []struct {
		lines []string
		want  string
	}{
		{[]string{"Warning: Permanently added '[h]:22' (ED25519) to the list of known hosts.",
			"deploy@h: Permission denied (publickey,password)."}, "ssh-copy-id"},
		{[]string{"bind [127.0.0.1]:5001: Address already in use", "channel_setup_fwd_listener_tcpip: cannot listen to port: 5001",
			"Could not request local forwarding."}, "порт занят"},
		{[]string{"Timeout, server 1.2.3.4 not responding."}, "связь потеряна"},
		{[]string{"ssh: connect to host h port 2222: Connection refused"}, "отказал"},
		{[]string{"ssh: Could not resolve hostname nope: Name or service not known"}, "имя сервера"},
		{[]string{"что-то новое"}, "ssh: что-то новое"},
	}
	for _, c := range cases {
		if got := Diagnose(c.lines, nil); !strings.Contains(got, c.want) {
			t.Errorf("%v → %q, ждали %q", c.lines, got, c.want)
		}
	}
	err := exec.Command("sh", "-c", "exit 255").Run()
	if got := Diagnose(nil, err); !strings.Contains(got, "255") {
		t.Errorf("код выхода потерян: %q", got)
	}
	if got := Diagnose(nil, errors.New("x")); !strings.Contains(got, "x") {
		t.Errorf("%q", got)
	}
}
