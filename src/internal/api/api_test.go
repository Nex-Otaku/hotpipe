package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nex-Otaku/hotpipe/internal/config"
	"github.com/Nex-Otaku/hotpipe/internal/tunnel"
)

func newServer(t *testing.T) (*Server, http.Handler) {
	path := filepath.Join(t.TempDir(), "tunnels.json")
	// /bin/false вместо ssh: здесь проверяется API, а не туннели.
	mgr := tunnel.NewManager("/bin/false")
	t.Cleanup(mgr.StopAll)
	s := New(path, "", config.Default(), mgr, func() {})
	return s, s.Handler()
}

func do(h http.Handler, method, url, body string, hdr bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:46310"+url, strings.NewReader(body))
	if hdr {
		r.Header.Set(Header, "1")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Порт берётся свободный, а не 5001: на машине разработчика на 5001 вполне
// может работать настоящий туннель.
var tunnelJSON = func() string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer ln.Close()
	return fmt.Sprintf(`{"name":"mysql","host":"203.0.113.10","port":2222,"user":"deploy","local_port":%d,"remote_port":3306,"enabled":false}`,
		ln.Addr().(*net.TCPAddr).Port)
}()

func TestGuard(t *testing.T) {
	_, h := newServer(t)
	if w := do(h, "POST", "/api/tunnels", tunnelJSON, false); w.Code != http.StatusForbidden {
		t.Fatalf("без заголовка: %d", w.Code)
	}
	r := httptest.NewRequest("GET", "http://evil.example:46310/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("чужой Host: %d", w.Code)
	}
	if w := do(h, "GET", "/api/status", "", false); w.Code != http.StatusOK {
		t.Fatalf("GET без заголовка должен работать: %d", w.Code)
	}
}

func TestCRUD(t *testing.T) {
	s, h := newServer(t)
	w := do(h, "POST", "/api/tunnels", tunnelJSON, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("создание: %d %s", w.Code, w.Body)
	}
	var created config.Tunnel
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" || created.LocalHost != "localhost" {
		t.Fatalf("%+v", created)
	}

	// Записано на диск сразу.
	f, err := config.Load(s.ConfigPath)
	if err != nil || len(f.Tunnels) != 1 {
		t.Fatalf("на диске: %v %+v", err, f)
	}

	if w := do(h, "POST", "/api/tunnels", tunnelJSON, true); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "уже занят") {
		t.Fatalf("второй туннель на тот же порт принят: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/tunnels", `{"host":"-oProxyCommand=x","local_port":1,"remote_port":1}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("опасный хост принят: %d", w.Code)
	}

	upd := strings.Replace(tunnelJSON, `"mysql"`, `"база"`, 1)
	if w := do(h, "PUT", "/api/tunnels/"+created.ID, upd, true); w.Code != http.StatusOK {
		t.Fatalf("правка: %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/tunnels/"+created.ID+"/start", "", true); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	var st StatusReply
	json.Unmarshal(do(h, "GET", "/api/status", "", false).Body.Bytes(), &st)
	if len(st.Tunnels) != 1 || st.Tunnels[0].Name != "база" || !st.Tunnels[0].Enabled || st.Tunnels[0].State == tunnel.Stopped {
		t.Fatalf("%+v", st.Tunnels)
	}
	if w := do(h, "POST", "/api/tunnels/"+created.ID+"/stop", "", true); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	f, _ = config.Load(s.ConfigPath)
	if f.Tunnels[0].Enabled {
		t.Fatal("выключение не запомнено")
	}
	if w := do(h, "DELETE", "/api/tunnels/"+created.ID, "", true); w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if w := do(h, "DELETE", "/api/tunnels/"+created.ID, "", true); w.Code != http.StatusNotFound {
		t.Fatalf("повторное удаление: %d", w.Code)
	}
	if w := do(h, "PUT", "/api/settings", `{"server_alive_interval":0,"server_alive_count_max":3}`, true); w.Code != http.StatusBadRequest {
		t.Fatalf("негодные настройки приняты: %d", w.Code)
	}
}
