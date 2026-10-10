package tunnel

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// В macOS нет /proc. Кто слушает порт, говорит lsof; родителя и аргументы
// процесса — ps (почему не подключением к порту — см. holder.go).
//
// Пути — полные: launchd запускает сервер с урезанным PATH.
const (
	lsofPath = "/usr/sbin/lsof"
	psPath   = "/bin/ps"
)

// listenerPIDs — процессы, слушающие порт по TCP (IPv4 и IPv6). Видны только
// процессы того же пользователя, но туннели и запускает тот же пользователь.
func listenerPIDs(port int, onlyPID int) []int {
	args := []string{"-nP", "-iTCP:" + strconv.Itoa(port), "-sTCP:LISTEN", "-t"}
	if onlyPID > 0 {
		// -a: условия «и», а не «или».
		args = append(args, "-a", "-p", strconv.Itoa(onlyPID))
	}
	// Ничего не нашедший lsof выходит с 1 — это не ошибка, а пустой ответ.
	out, _ := exec.Command(lsofPath, args...).Output()
	return parsePIDs(string(out))
}

// parsePIDs разбирает вывод «lsof -t»: по pid в строке.
func parsePIDs(s string) []int {
	var out []int
	for _, l := range strings.Fields(s) {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// Listening сообщает, слушает ли процесс pid порт port.
func Listening(pid, port int) bool {
	for _, p := range listenerPIDs(port, pid) {
		if p == pid {
			return true
		}
	}
	return false
}

// PortHolder ищет процесс, слушающий порт.
func PortHolder(port int) (Holder, bool) {
	pids := listenerPIDs(port, 0)
	if len(pids) == 0 {
		return Holder{}, false
	}
	return describe(pids[0]), true
}

func ps(pid int, field string) string {
	out, err := exec.Command(psPath, "-ww", "-o", field+"=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func describe(pid int) Holder {
	h := Holder{PID: pid}
	h.Comm = filepath.Base(ps(pid, "comm"))
	h.PPID, _ = strconv.Atoi(ps(pid, "ppid"))
	h.Cmdline = ps(pid, "args")
	parseMarkerArgs(strings.Fields(h.Cmdline), &h)
	return h
}

// В macOS окружение чужого процесса не прочесть: для ssh и прочих программ
// Apple система его не отдаёт даже тому же пользователю. Поэтому метка — в
// аргументах ssh, параметром Tag: ssh его только запоминает (для «Match
// tagged» в ~/.ssh/config) и ни на что больше он не влияет (Р-011).
const tagPrefix = "Tag=hotpipe:"

func markerEnv(v string) []string  { return nil }
func markerArgs(v string) []string { return []string{"-o", tagPrefix + v} }

// parseMarkerArgs ищет метку в аргументах. Тег из командной строки ssh
// принимает только такой, как мы его передаём: «-o Tag=hotpipe:…».
func parseMarkerArgs(args []string, h *Holder) {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, tagPrefix); ok && i > 0 && args[i-1] == "-o" {
			parseMarker(v, h)
			return
		}
	}
}

// sysProcAttr — как запускать ssh.
//
// Своя группа — чтобы сигнал дошёл и до того, что ssh мог запустить
// (ProxyCommand из ~/.ssh/config). Pdeathsig, как в Linux, в macOS нет: ssh
// упавшего сервера остаётся держать порт. Его убирает следующий сервер — по
// метке он сирота, — а следующий поднимает launchd (Р-011).
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
