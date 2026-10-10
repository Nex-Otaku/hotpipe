package tunnel

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

// Поддельный ssh: сам тестовый двоичный файл, запущенный с FAKE_SSH. Разбирает
// -L, слушает локальный порт и ведёт себя так, как велит FAKE_SSH.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_SSH"); mode != "" {
		fakeSSH(mode)
		return
	}
	os.Exit(m.Run())
}

func fakeSSH(mode string) {
	if mode == "deny" {
		fmt.Fprintln(os.Stderr, "u@h: Permission denied (publickey).")
		os.Exit(255)
	}
	var fwd string
	for i, a := range os.Args {
		if a == "-L" {
			fwd = os.Args[i+1]
		}
	}
	parts := strings.Split(fwd, ":")
	ln, err := net.Listen("tcp", "127.0.0.1:"+parts[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "bind [127.0.0.1]:%s: Address already in use\n", parts[1])
		os.Exit(255)
	}
	defer ln.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	switch mode {
	case "drop": // поработать и оборваться, как при потере связи
		select {
		case <-time.After(700 * time.Millisecond):
		case <-sig:
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "Timeout, server h not responding.")
		os.Exit(255)
	default: // "ok": работать, пока не попросят
		<-sig
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func fastTimings(t *testing.T) {
	old := backoff
	backoff = []time.Duration{50 * time.Millisecond}
	pc := pollConnecting
	pollConnecting = 20 * time.Millisecond
	pu := pollUp
	pollUp = 50 * time.Millisecond
	t.Cleanup(func() { backoff, pollConnecting, pollUp = old, pc, pu })
}

func tunnelOn(port int) config.Tunnel {
	tn := config.Tunnel{ID: "t1", Name: "проба", Enabled: true, Host: "h", LocalHost: "127.0.0.1",
		LocalPort: port, RemotePort: 1}
	tn.Normalize()
	return tn
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func newFile(tn ...config.Tunnel) config.File {
	f := config.Default()
	f.Tunnels = tn
	return f
}

func TestUpAndStop(t *testing.T) {
	fastTimings(t)
	t.Setenv("FAKE_SSH", "ok")
	port := freePort(t)
	m := NewManager(os.Args[0])
	m.Apply(newFile(tunnelOn(port)))
	waitFor(t, "поднят", func() bool { return m.Statuses()[0].State == Up })

	pid := m.Statuses()[0].PID
	off := tunnelOn(port)
	off.Enabled = false
	m.Apply(newFile(off))
	if st := m.Statuses()[0]; st.State != Stopped {
		t.Fatalf("не остановлен: %+v", st)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatalf("ssh %d пережил остановку", pid)
	}
}

// Оборвавшийся ssh поднимается заново сам — ради этого всё и затевалось.
func TestRevivesAfterDrop(t *testing.T) {
	fastTimings(t)
	t.Setenv("FAKE_SSH", "drop")
	m := NewManager(os.Args[0])
	m.Apply(newFile(tunnelOn(freePort(t))))
	defer m.StopAll()
	waitFor(t, "два перезапуска", func() bool { return m.Statuses()[0].Restarts >= 2 })
	st := m.Statuses()[0]
	if !strings.Contains(st.LastError, "связь потеряна") {
		t.Fatalf("ошибка не объяснена: %q", st.LastError)
	}
}

func TestDeniedIsExplained(t *testing.T) {
	fastTimings(t)
	t.Setenv("FAKE_SSH", "deny")
	m := NewManager(os.Args[0])
	m.Apply(newFile(tunnelOn(freePort(t))))
	defer m.StopAll()
	waitFor(t, "ошибка", func() bool { return m.Statuses()[0].LastError != "" })
	st := m.Statuses()[0]
	if !strings.Contains(st.LastError, "ssh-copy-id") || len(st.Output) == 0 {
		t.Fatalf("%+v", st)
	}
}

// Порт, занятый чужим процессом, не трогается, а называется по имени; ssh при
// этом не запускается вовсе.
func TestForeignHolderNamed(t *testing.T) {
	fastTimings(t)
	t.Setenv("FAKE_SSH", "ok")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	m := NewManager(os.Args[0])
	m.Apply(newFile(tunnelOn(port)))
	defer m.StopAll()
	waitFor(t, "ошибка", func() bool { return m.Statuses()[0].LastError != "" })
	st := m.Statuses()[0]
	if !strings.Contains(st.LastError, fmt.Sprintf("pid %d", os.Getpid())) {
		t.Fatalf("держатель не назван: %q", st.LastError)
	}
}

// Осиротевший ssh от упавшего сервера (помечен нашей переменной) убирается, и
// туннель поднимается на его месте.
func TestOrphanReplaced(t *testing.T) {
	fastTimings(t)
	port := freePort(t)
	tn := tunnelOn(port)
	// В метке — pid сервера, которого уже нет: родитель сироты не он.
	orphan := execFake(t, tn, "999999999:"+tn.ID)
	waitFor(t, "сирота слушает", func() bool { return Listening(orphan, port) })

	t.Setenv("FAKE_SSH", "ok")
	m := NewManager(os.Args[0])
	m.Apply(newFile(tn))
	defer m.StopAll()
	waitFor(t, "поднят новым ssh", func() bool {
		st := m.Statuses()[0]
		return st.State == Up && st.PID != orphan
	})
}

// ssh другого, живого сервера (родитель совпадает с меткой) не трогается —
// иначе второй экземпляр или прогон проверок ронял бы работающие туннели.
func TestSiblingNotKilled(t *testing.T) {
	fastTimings(t)
	port := freePort(t)
	tn := tunnelOn(port)
	sibling := execFake(t, tn, markerValue(tn.ID))
	waitFor(t, "соседний ssh слушает", func() bool { return Listening(sibling, port) })

	t.Setenv("FAKE_SSH", "ok")
	m := NewManager(os.Args[0])
	m.Apply(newFile(tn))
	defer m.StopAll()
	waitFor(t, "ошибка", func() bool { return m.Statuses()[0].LastError != "" })
	if st := m.Statuses()[0]; !strings.Contains(st.LastError, "другого работающего") {
		t.Fatalf("%q", st.LastError)
	}
	if syscall.Kill(sibling, 0) != nil {
		t.Fatal("ssh живого сервера убит")
	}
}

func execFake(t *testing.T, tn config.Tunnel, marker string) int {
	attr := &os.ProcAttr{
		Env:   append(append(os.Environ(), "FAKE_SSH=ok"), markerEnv(marker)...),
		Files: []*os.File{nil, nil, os.Stderr},
	}
	args := append(append([]string{os.Args[0]}, markerArgs(marker)...), Args(tn, config.Default().Settings)...)
	p, err := os.StartProcess(os.Args[0], args, attr)
	if err != nil {
		t.Fatal(err)
	}
	go p.Wait()
	t.Cleanup(func() { p.Kill() })
	return p.Pid
}

// Правка имени не перезапускает туннель, правка порта — перезапускает.
func TestApplyKeepsUnchanged(t *testing.T) {
	fastTimings(t)
	t.Setenv("FAKE_SSH", "ok")
	m := NewManager(os.Args[0])
	tn := tunnelOn(freePort(t))
	m.Apply(newFile(tn))
	defer m.StopAll()
	waitFor(t, "поднят", func() bool { return m.Statuses()[0].State == Up })
	pid := m.Statuses()[0].PID

	tn.Name = "новое имя"
	m.Apply(newFile(tn))
	if st := m.Statuses()[0]; st.PID != pid || st.Name != "новое имя" {
		t.Fatalf("переименование перезапустило туннель или не дошло: %+v", st)
	}
	tn.LocalPort = freePort(t)
	m.Apply(newFile(tn))
	waitFor(t, "поднят на новом порту", func() bool {
		st := m.Statuses()[0]
		return st.State == Up && st.PID != pid
	})
}
