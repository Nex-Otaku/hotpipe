package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

// State — состояние туннеля.
type State string

const (
	Stopped    State = "stopped"    // выключен
	Connecting State = "connecting" // ssh запущен, порт ещё не слушается
	Up         State = "up"         // порт слушается самим ssh
	Waiting    State = "waiting"    // сбой; ждём следующей попытки
)

// Status — то, что видно снаружи.
type Status struct {
	config.Tunnel
	State State     `json:"state"`
	PID   int       `json:"pid,omitempty"`
	Since time.Time `json:"since"` // с какого момента в нынешнем состоянии
	// Restarts — сколько раз туннель поднимался заново с момента включения.
	Restarts  int        `json:"restarts"`
	LastError string     `json:"last_error,omitempty"`
	ErrorAt   *time.Time `json:"error_at,omitempty"`
	RetryAt   *time.Time `json:"retry_at,omitempty"`
	// Output — последние строки ssh: перевод в Diagnose их не заменяет.
	Output []string `json:"output"`
}

// Сроки.
var (
	// Повторы после сбоя. Первые — быстро: обрыв из-за заминки сети чинится
	// сразу. Дальше — реже: сервер, отвергающий ключ, незачем дёргать каждую
	// секунду сутками напролёт.
	backoff = []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second,
		10 * time.Second, 30 * time.Second, 60 * time.Second}

	// Сколько туннель должен простоять, чтобы следующий обрыв снова чинился
	// быстро, а не после минутной паузы.
	stableAfter = 60 * time.Second

	// Сколько ждать, пока ssh откроет порт. ConnectTimeout у ssh — 15 с, но
	// после подключения бывает долгий обмен ключами на медленной связи.
	connectLimit = 45 * time.Second

	// Как часто проверять, слушает ли ssh порт.
	pollConnecting = 300 * time.Millisecond
	pollUp         = 2 * time.Second

	// Сколько ждать мягкого завершения ssh, прежде чем убить.
	killGrace = 3 * time.Second
)

const outputLines = 30

type runner struct {
	ssh  string
	spec config.Tunnel
	args []string

	cancel context.CancelFunc
	done   chan struct{}
	kick   chan struct{}

	mu sync.Mutex
	st Status
}

func newRunner(ssh string, spec config.Tunnel, args []string) *runner {
	return &runner{
		ssh:  ssh,
		spec: spec,
		args: args,
		done: make(chan struct{}),
		kick: make(chan struct{}, 1),
		st:   Status{Tunnel: spec, State: Connecting, Since: time.Now(), Output: []string{}},
	}
}

func (r *runner) logf(format string, a ...any) {
	r.mu.Lock()
	name := r.st.Name
	r.mu.Unlock()
	log.Printf("[%s] "+format, append([]any{name}, a...)...)
}

func (r *runner) status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.st
	s.Output = append([]string(nil), r.st.Output...)
	return s
}

func (r *runner) setState(st State, pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st.State, r.st.PID, r.st.Since, r.st.RetryAt = st, pid, time.Now(), nil
}

func (r *runner) addOutput(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.st.Output = append(r.st.Output, line)
	if n := len(r.st.Output); n > outputLines {
		r.st.Output = r.st.Output[n-outputLines:]
	}
}

func (r *runner) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.loop(ctx)
}

// stop останавливает туннель и ждёт, пока ssh действительно завершится: иначе
// новый ssh с тем же портом упёрся бы в ещё не освобождённый.
func (r *runner) stop() {
	r.cancel()
	<-r.done
}

// retryNow прерывает ожидание следующей попытки.
func (r *runner) retryNow() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *runner) loop(ctx context.Context) {
	defer close(r.done)
	defer r.setState(Stopped, 0)

	step := 0
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			r.mu.Lock()
			r.st.Restarts++
			r.mu.Unlock()
		}
		res := r.once(ctx)
		if ctx.Err() != nil {
			return
		}
		if res.upFor >= stableAfter {
			step = 0
		}
		delay := backoff[min(step, len(backoff)-1)]
		step++
		if res.retryNow {
			delay = 500 * time.Millisecond
		}

		now := time.Now()
		retry := now.Add(delay)
		r.mu.Lock()
		r.st.State, r.st.PID, r.st.Since = Waiting, 0, now
		r.st.LastError, r.st.ErrorAt, r.st.RetryAt = res.err, &now, &retry
		r.mu.Unlock()
		r.logf("%s; следующая попытка через %s", res.err, delay)

		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-r.kick:
			t.Stop()
		}
	}
}

type attempt struct {
	err      string
	upFor    time.Duration
	retryNow bool
}

// once — одна жизнь процесса ssh: от запуска до выхода.
func (r *runner) once(ctx context.Context) attempt {
	// Порт проверяем до запуска. Занятый порт ssh всё равно не откроет, а
	// подключение к серверу ради этого — лишний вход в чужой журнал входов.
	if h, busy := PortHolder(r.spec.LocalPort); busy {
		if h.Orphan() {
			// Осиротевший ssh от упавшего экземпляра сервера: он наш, и порт
			// держит зря. Чужой процесс — например, ssh, запущенный руками, или
			// ssh другого работающего сервера, — не трогаем.
			r.logf("порт %d держит прежний ssh %s — завершаю его", r.spec.LocalPort, h)
			_ = syscall.Kill(h.PID, syscall.SIGTERM)
			return attempt{err: fmt.Sprintf("порт %d держал прежний ssh (pid %d), завершён", r.spec.LocalPort, h.PID), retryNow: true}
		}
		if h.Tunnel != "" {
			return attempt{err: fmt.Sprintf("локальный порт %d занят туннелем другого работающего hotpipe-server (pid %d)", r.spec.LocalPort, h.Server)}
		}
		return attempt{err: fmt.Sprintf("локальный порт %d занят: %s — %s", r.spec.LocalPort, h, h.Cmdline)}
	}

	mark := markerValue(r.spec.ID)
	cmd := exec.Command(r.ssh, append(markerArgs(mark), r.args...)...)
	cmd.Env = append(os.Environ(), markerEnv(mark)...)
	cmd.SysProcAttr = sysProcAttr()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return attempt{err: "не открыть вывод ssh: " + err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return attempt{err: "не запустить ssh: " + err.Error()}
	}
	pid := cmd.Process.Pid
	r.setState(Connecting, pid)
	r.logf("запущен ssh, pid %d", pid)

	var lines []string
	var linesMu sync.Mutex
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			l := sc.Text()
			linesMu.Lock()
			lines = append(lines, l)
			linesMu.Unlock()
			r.addOutput(l)
			r.logf("ssh: %s", l)
		}
		_, _ = io.Copy(io.Discard, stderr)
	}()
	exited := make(chan error, 1)
	go func() {
		// Wait — только после того, как вывод дочитан: так велит os/exec.
		<-readDone
		exited <- cmd.Wait()
	}()

	var upAt time.Time
	deadline := time.Now().Add(connectLimit)
	tick := time.NewTimer(pollConnecting)
	defer tick.Stop()

	upFor := func() time.Duration {
		if upAt.IsZero() {
			return 0
		}
		return time.Since(upAt)
	}
	for {
		select {
		case err := <-exited:
			linesMu.Lock()
			msg := Diagnose(lines, err)
			linesMu.Unlock()
			return attempt{err: msg, upFor: upFor()}

		case <-ctx.Done():
			terminate(pid, exited)
			r.logf("остановлен")
			return attempt{}

		case <-tick.C:
			listening := Listening(pid, r.spec.LocalPort)
			switch {
			case upAt.IsZero() && listening:
				upAt = time.Now()
				r.setState(Up, pid)
				r.logf("поднят: порт %d", r.spec.LocalPort)
			case upAt.IsZero() && time.Now().After(deadline):
				terminate(pid, exited)
				return attempt{err: fmt.Sprintf("ssh не открыл порт за %s", connectLimit)}
			case !upAt.IsZero() && !listening:
				// Уходящий ssh закрывает порт чуть раньше, чем выходит. Даём ему
				// выйти самому — иначе вместо его объяснения («сервер не
				// отвечает») осталось бы наше, ничего не говорящее.
				select {
				case err := <-exited:
					linesMu.Lock()
					msg := Diagnose(lines, err)
					linesMu.Unlock()
					return attempt{err: msg, upFor: upFor()}
				case <-time.After(time.Second):
				}
				terminate(pid, exited)
				return attempt{err: "ssh перестал слушать порт", upFor: upFor()}
			}
			if upAt.IsZero() {
				tick.Reset(pollConnecting)
			} else {
				tick.Reset(pollUp)
			}
		}
	}
}

// terminate просит группу ssh завершиться, а не послушавшую — убивает.
func terminate(pid int, exited <-chan error) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(killGrace):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-exited
	}
}
