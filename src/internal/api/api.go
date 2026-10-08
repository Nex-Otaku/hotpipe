// Package api — HTTP API сервера: состояние туннелей и управление ими.
//
// Слушается только петлевой адрес. Из Windows он виден по 127.0.0.1 тоже —
// WSL пробрасывает петлевые порты гостя наружу, — и окну этого достаточно.
//
// Изменяющие запросы требуют заголовка X-Hotpipe. Без него любая страница в
// браузере могла бы отправить POST на 127.0.0.1 — простые запросы браузер
// отправляет без спроса. Нестандартный заголовок вынуждает браузер сначала
// спросить разрешения (CORS), а мы его не даём. Проверка Host закрывает
// подмену через DNS rebinding (Р-006).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Nex-Otaku/hotpipe/internal/config"
	"github.com/Nex-Otaku/hotpipe/internal/tunnel"
	"github.com/Nex-Otaku/hotpipe/internal/version"
)

// DefaultPort — порт API. Рядом с портами wsldrive (46201, 46202), но не
// вплотную, и вне диапазона временных портов (49152 и выше).
const DefaultPort = 46310

// Header — заголовок, без которого изменяющие запросы отвергаются.
const Header = "X-Hotpipe"

// Server — состояние и обработчики API.
type Server struct {
	ConfigPath string
	LogPath    string
	Mgr        *tunnel.Manager
	// Shutdown вызывается по POST /api/shutdown.
	Shutdown func()

	started time.Time

	mu  sync.Mutex
	cfg config.File
}

// New — сервер с уже загруженными настройками; туннели поднимаются сразу.
func New(path, logPath string, cfg config.File, mgr *tunnel.Manager, shutdown func()) *Server {
	s := &Server{ConfigPath: path, LogPath: logPath, Mgr: mgr, Shutdown: shutdown, started: time.Now(), cfg: cfg}
	mgr.Apply(cfg)
	return s
}

// Reload перечитывает файл настроек (SIGHUP).
func (s *Server) Reload() error {
	f, err := config.Load(s.ConfigPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg = f
	s.mu.Unlock()
	s.Mgr.Apply(f)
	return nil
}

// change применяет правку: к копии, с проверкой, с записью на диск — и только
// после удачной записи к работающим туннелям. Иначе после перезапуска
// сервер поднял бы не то, что показывал.
func (s *Server) change(edit func(*config.File) error) (config.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.Clone()
	if err := edit(&next); err != nil {
		return config.File{}, err
	}
	if err := next.Validate(); err != nil {
		return config.File{}, err
	}
	if err := config.Save(s.ConfigPath, next); err != nil {
		return config.File{}, err
	}
	s.cfg = next
	s.Mgr.Apply(next)
	return next, nil
}

// Handler — маршруты API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.text)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/log", s.log)
	mux.HandleFunc("POST /api/tunnels", s.create)
	mux.HandleFunc("PUT /api/tunnels/{id}", s.update)
	mux.HandleFunc("DELETE /api/tunnels/{id}", s.remove)
	mux.HandleFunc("POST /api/tunnels/{id}/{action}", s.action)
	mux.HandleFunc("PUT /api/settings", s.settings)
	mux.HandleFunc("POST /api/retry", s.retry)
	mux.HandleFunc("POST /api/reload", s.reload)
	mux.HandleFunc("POST /api/shutdown", s.shutdown)
	return guard(mux)
}

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		switch strings.Trim(host, "[]") {
		case "127.0.0.1", "localhost", "::1":
		default:
			writeErr(w, http.StatusForbidden, errors.New("API доступен только по 127.0.0.1"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(Header) == "" {
			writeErr(w, http.StatusForbidden, fmt.Errorf("изменяющий запрос без заголовка %s отвергнут", Header))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON отдаёт ответ с Content-Length: без него Go разбивает длинный ответ
// на куски (chunked), а простейший клиент окна их не собирает.
func writeJSON(w http.ResponseWriter, code int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	w.WriteHeader(code)
	_, _ = w.Write(buf.Bytes())
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// StatusReply — ответ /api/status.
type StatusReply struct {
	Version  string          `json:"version"`
	Build    string          `json:"build"`
	PID      int             `json:"pid"`
	Started  time.Time       `json:"started"`
	Now      time.Time       `json:"now"`
	Config   string          `json:"config"`
	Log      string          `json:"log"`
	Settings config.Settings `json:"settings"`
	Tunnels  []tunnel.Status `json:"tunnels"`
}

func (s *Server) snapshot() StatusReply {
	s.mu.Lock()
	set := s.cfg.Settings
	s.mu.Unlock()
	return StatusReply{
		Version:  version.Number,
		Build:    version.String("hotpipe-server"),
		PID:      os.Getpid(),
		Started:  s.started,
		Now:      time.Now(),
		Config:   s.ConfigPath,
		Log:      s.LogPath,
		Settings: set,
		Tunnels:  s.Mgr.Statuses(),
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.snapshot())
}

// text — то же для человека с curl.
func (s *Server) text(w http.ResponseWriter, r *http.Request) {
	st := s.snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nработает %s, настройки %s\n\n", st.Build,
		time.Since(st.Started).Round(time.Second), st.Config)
	if len(st.Tunnels) == 0 {
		b.WriteString("туннелей нет\n")
	}
	for _, t := range st.Tunnels {
		fmt.Fprintf(&b, "%-10s %-24s %s:%d -> %s:%d через %s@%s:%d",
			t.State, t.Name, t.LocalHost, t.LocalPort, t.RemoteHost, t.RemotePort, t.User, t.Host, t.Port)
		if t.State == tunnel.Up {
			fmt.Fprintf(&b, ", %s", time.Since(t.Since).Round(time.Second))
		}
		if t.Restarts > 0 {
			fmt.Fprintf(&b, ", перезапусков %d", t.Restarts)
		}
		b.WriteString("\n")
		if t.State == tunnel.Waiting && t.LastError != "" {
			fmt.Fprintf(&b, "           %s\n", t.LastError)
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, b.String())
}

// log — хвост журнала сервера: окну незачем лезть за ним в WSL.
func (s *Server) log(w http.ResponseWriter, r *http.Request) {
	const maxTail = 64 << 10
	if s.LogPath == "" {
		writeJSON(w, http.StatusOK, map[string]string{"log": "журнал отключён"})
		return
	}
	f, err := os.Open(s.LogPath)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxTail {
		_, _ = f.Seek(fi.Size()-maxTail, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	text := string(b)
	if i := strings.IndexByte(text, '\n'); len(b) == maxTail && i >= 0 {
		text = text[i+1:] // первая строка обрезана посередине
	}
	writeJSON(w, http.StatusOK, map[string]string{"log": text, "path": s.LogPath})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("запрос не разобран: %w", err)
	}
	return nil
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var t config.Tunnel
	if err := decode(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t.ID = config.NewID()
	t.Normalize()
	if _, err := s.change(func(f *config.File) error {
		f.Tunnels = append(f.Tunnels, t)
		return nil
	}); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var t config.Tunnel
	if err := decode(r, &t); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t.ID = id
	t.Normalize()
	if _, err := s.change(func(f *config.File) error {
		i := f.Find(id)
		if i < 0 {
			return errNotFound
		}
		f.Tunnels[i] = t
		return nil
	}); err != nil {
		writeErr(w, codeFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.change(func(f *config.File) error {
		i := f.Find(id)
		if i < 0 {
			return errNotFound
		}
		f.Tunnels = append(f.Tunnels[:i], f.Tunnels[i+1:]...)
		return nil
	}); err != nil {
		writeErr(w, codeFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// action — start, stop, restart.
//
// start и stop меняют желаемое состояние и записывают его: выключенный руками
// туннель не должен подняться сам после перезагрузки.
func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	switch act := r.PathValue("action"); act {
	case "start", "stop":
		if _, err := s.change(func(f *config.File) error {
			i := f.Find(id)
			if i < 0 {
				return errNotFound
			}
			f.Tunnels[i].Enabled = act == "start"
			return nil
		}); err != nil {
			writeErr(w, codeFor(err), err)
			return
		}
	case "restart":
		if !s.Mgr.Restart(id) {
			writeErr(w, http.StatusConflict, errors.New("туннель выключен — перезапускать нечего"))
			return
		}
	default:
		writeErr(w, http.StatusNotFound, fmt.Errorf("нет действия %q; есть start, stop, restart", act))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": id})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	var set config.Settings
	if err := decode(r, &set); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.change(func(f *config.File) error {
		f.Settings = set
		return nil
	}); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) retry(w http.ResponseWriter, r *http.Request) {
	s.Mgr.RetryNow()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	if err := s.Reload(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) shutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.Shutdown()
}

var errNotFound = errors.New("нет такого туннеля")

func codeFor(err error) int {
	if errors.Is(err, errNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// Serve отвечает на запросы, пока не отменён ctx.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
