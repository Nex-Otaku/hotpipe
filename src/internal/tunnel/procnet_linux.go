package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Поднят ли туннель, узнаём по /proc: слушает ли сам процесс ssh свой
// локальный порт (почему не подключением — см. holder.go).

// procRoot — корень /proc; в проверках подменяется.
var procRoot = "/proc"

// socketInodes — номера сокетов, открытых процессом.
func socketInodes(pid int) (map[uint64]bool, error) {
	dir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[uint64]bool{}
	for _, e := range ents {
		link, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if s, ok := strings.CutPrefix(link, "socket:["); ok {
			if n, err := strconv.ParseUint(strings.TrimSuffix(s, "]"), 10, 64); err == nil {
				out[n] = true
			}
		}
	}
	return out, nil
}

// listeners — номера сокетов, слушающих порт по TCP (IPv4 и IPv6).
func listeners(port int) map[uint64]bool {
	out := map[uint64]bool{}
	for _, name := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(procRoot, "net", name))
		if err != nil {
			continue
		}
		parseListeners(bufio.NewScanner(f), port, out)
		f.Close()
	}
	return out
}

// parseListeners разбирает таблицу /proc/net/tcp{,6}:
//
//	sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
//	 0: 0100007F:1389 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 41234 …
//
// Состояние 0A — LISTEN.
func parseListeners(sc *bufio.Scanner, port int, out map[uint64]bool) {
	want := fmt.Sprintf(":%04X", port)
	first := true
	for sc.Scan() {
		if first { // заголовок
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != "0A" || !strings.HasSuffix(f[1], want) {
			continue
		}
		if n, err := strconv.ParseUint(f[9], 10, 64); err == nil && n != 0 {
			out[n] = true
		}
	}
}

// Listening сообщает, слушает ли процесс pid порт port.
func Listening(pid, port int) bool {
	ls := listeners(port)
	if len(ls) == 0 {
		return false
	}
	own, err := socketInodes(pid)
	if err != nil {
		return false
	}
	for n := range ls {
		if own[n] {
			return true
		}
	}
	return false
}

// PortHolder ищет процесс, слушающий порт. Видны только процессы того же
// пользователя — для чужих /proc/<pid>/fd закрыт, — но туннели и запускает
// тот же пользователь.
func PortHolder(port int) (Holder, bool) {
	ls := listeners(port)
	if len(ls) == 0 {
		return Holder{}, false
	}
	procs, err := os.ReadDir(procRoot)
	if err != nil {
		return Holder{}, false
	}
	for _, p := range procs {
		pid, err := strconv.Atoi(p.Name())
		if err != nil {
			continue
		}
		own, err := socketInodes(pid)
		if err != nil {
			continue
		}
		for n := range ls {
			if own[n] {
				return describe(pid), true
			}
		}
	}
	return Holder{}, false
}

// sysProcAttr — как запускать ssh.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		// Своя группа — чтобы сигнал дошёл и до того, что ssh мог запустить
		// (ProxyCommand из ~/.ssh/config).
		Setpgid: true,
		// Упади сервер сам — ssh уйдёт за ним и не останется держать порт.
		Pdeathsig: syscall.SIGTERM,
	}
}

// В Linux метка — в окружении ssh: /proc/<pid>/environ его показывает.
func markerEnv(v string) []string  { return []string{envMarker + "=" + v} }
func markerArgs(v string) []string { return nil }

func describe(pid int) Holder {
	h := Holder{PID: pid}
	base := filepath.Join(procRoot, strconv.Itoa(pid))
	if b, err := os.ReadFile(filepath.Join(base, "comm")); err == nil {
		h.Comm = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile(filepath.Join(base, "cmdline")); err == nil {
		h.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
	}
	// stat: «pid (comm) state ppid …»; comm может содержать пробелы и скобки,
	// поэтому считаем от последней закрывающей скобки.
	if b, err := os.ReadFile(filepath.Join(base, "stat")); err == nil {
		if i := strings.LastIndexByte(string(b), ')'); i >= 0 {
			if f := strings.Fields(string(b)[i+1:]); len(f) >= 2 {
				h.PPID, _ = strconv.Atoi(f[1])
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(base, "environ")); err == nil {
		for _, kv := range strings.Split(string(b), "\x00") {
			if v, ok := strings.CutPrefix(kv, envMarker+"="); ok {
				parseMarker(v, &h)
			}
		}
	}
	return h
}
