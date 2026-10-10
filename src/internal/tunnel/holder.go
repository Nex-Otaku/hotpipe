package tunnel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Поднят ли туннель, узнаём не подключением к порту, а у системы: слушает ли
// сам процесс ssh свой локальный порт. Подключение к -L туннелю ssh тут же
// передаёт на ту сторону, и каждая проверка была бы настоящим соединением с
// удалённой службой. MySQL, например, считает такие оборванные соединения
// ошибками и после сотни (max_connect_errors) блокирует адрес — а адресом этим
// для неё был бы её же 127.0.0.1 (Р-004).
//
// Как спросить систему, зависит от неё: procnet_linux.go читает /proc,
// procnet_darwin.go спрашивает lsof и ps.

// Holder — кто держит порт.
type Holder struct {
	PID     int
	Comm    string
	Cmdline string
	PPID    int
	// Tunnel и Server — из метки: туннель и pid сервера, запустившего этот
	// ssh. Пусто — процесс не наш.
	Tunnel string
	Server int
}

// Orphan — наш ssh, переживший свой сервер.
//
// Метки мало: ssh с меткой может принадлежать живому серверу — второму
// экземпляру или проверкам, — и убить его значило бы уронить чужой работающий
// туннель. Так однажды прогон тестов и уронил настоящий туннель. Сирота — тот,
// чей родитель уже не записанный в метке сервер: умерший сервер передаёт своих
// детей init.
func (h Holder) Orphan() bool {
	return h.Tunnel != "" && h.PPID != h.Server
}

func (h Holder) String() string {
	return fmt.Sprintf("%s (pid %d)", h.Comm, h.PID)
}

// Метка наших ssh: <pid сервера>:<id туннеля>. По ней осиротевший ssh от
// упавшего сервера отличается от ssh, запущенного человеком руками: первый
// можно убрать, второй трогать нельзя.
//
// Где метка носится, зависит от системы (markerEnv, markerArgs): в Linux — в
// переменной окружения envMarker, в macOS — в аргументах ssh (Р-010).
const envMarker = "HOTPIPE_TUNNEL"

func markerValue(id string) string {
	return fmt.Sprintf("%d:%s", os.Getpid(), id)
}

// parseMarker разбирает значение метки.
func parseMarker(v string, h *Holder) {
	if srv, id, ok := strings.Cut(v, ":"); ok {
		h.Server, _ = strconv.Atoi(srv)
		h.Tunnel = id
	}
}
