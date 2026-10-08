package tunnel

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

// Живая проверка: настоящий ssh до настоящего сервера. Остальные проверки
// обходятся поддельным ssh; эта нужна, чтобы убедиться, что ssh принимает наши
// аргументы и туннель действительно поднимается.
//
// Адрес сервера — личное дело каждого и в хранилище не попадает: берётся из
// окружения или из .env в корне проекта (образец — .env.example, сам .env
// перечислен в .gitignore). Без HOTPIPE_LIVE_HOST проверка пропускается.
func TestLiveTunnel(t *testing.T) {
	env := liveEnv(t)
	host := env("HOTPIPE_LIVE_HOST")
	if host == "" {
		t.Skip("HOTPIPE_LIVE_HOST не задан (см. .env.example)")
	}
	tn := config.Tunnel{ID: "live", Name: "живая проверка", Enabled: true,
		Host: host, User: env("HOTPIPE_LIVE_USER"),
		Port:       atoi(env("HOTPIPE_LIVE_PORT")),
		RemoteHost: env("HOTPIPE_LIVE_REMOTE_HOST"),
		RemotePort: atoi(env("HOTPIPE_LIVE_REMOTE_PORT")),
		LocalHost:  "127.0.0.1",
		// Свободный порт, а не рабочий: живой туннель на рабочем порту уже
		// может держать настоящий сервер.
		LocalPort: freePort(t),
	}
	tn.Normalize()
	if err := tn.Validate(); err != nil {
		t.Fatalf(".env: %v", err)
	}
	m := NewManager("ssh")
	m.Apply(newFile(tn))
	defer m.StopAll()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		st := m.Statuses()[0]
		if st.State == Up {
			return
		}
		if st.LastError != "" {
			t.Fatalf("%s\n%s", st.LastError, strings.Join(st.Output, "\n"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("туннель не поднялся за 45 с")
}

// liveEnv — значения из окружения, а чего там нет — из .env в корне проекта.
func liveEnv(t *testing.T) func(string) string {
	file := map[string]string{}
	if f, err := os.Open(filepath.Join("..", "..", "..", ".env")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if k, v, ok := strings.Cut(l, "="); ok && !strings.HasPrefix(l, "#") {
				file[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	return func(k string) string {
		if v, ok := os.LookupEnv(k); ok {
			return v
		}
		return file[k]
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
