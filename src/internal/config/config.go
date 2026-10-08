// Package config — описание туннелей в JSON.
//
// Файл один: в нём и туннели, и общие настройки ssh. Правят его сервер (по
// запросам окна) и, при желании, человек руками — после ручной правки сервер
// перечитывает файл по SIGHUP.
//
// Каждое поле, которое попадает в командную строку ssh, проверяется строго.
// Это не придирка: HTTP API доступен любой программе на машине, в том числе
// странице в браузере, и хост вида «-oProxyCommand=…» превратился бы в запуск
// произвольной команды (Р-006).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Tunnel — один туннель: перенаправление локального порта на порт удалённой
// стороны через ssh-сервер. То же, что ssh -L local_host:local_port:remote_host:remote_port.
type Tunnel struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Enabled — желаемое состояние: поднимать ли туннель. Запоминается, чтобы
	// туннель, выключенный руками, не поднимался сам после перезагрузки.
	Enabled bool `json:"enabled"`

	// Куда подключается ssh.
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`

	// Какой порт открыть здесь. Пустой адрес — localhost: так же, как у
	// ssh -L 5001:…, слушаются и 127.0.0.1, и ::1.
	LocalHost string `json:"local_host"`
	LocalPort int    `json:"local_port"`

	// Куда ведёт туннель — адрес с точки зрения ssh-сервера.
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`

	// IdentityFile — ключ; пусто — те, что ssh найдёт сам.
	IdentityFile string `json:"identity_file,omitempty"`
}

// Settings — общие для всех туннелей настройки ssh.
type Settings struct {
	// Как часто спрашивать сервер, жив ли он, и сколько неответов терпеть.
	// Произведение — время, за которое обнаруживается оборванная связь.
	ServerAliveInterval int `json:"server_alive_interval"`
	ServerAliveCountMax int `json:"server_alive_count_max"`
}

// File — содержимое файла целиком.
type File struct {
	Settings Settings `json:"settings"`
	Tunnels  []Tunnel `json:"tunnels"`
}

// По умолчанию обрыв обнаруживается за 15 секунд. ServerAliveInterval=1 из
// привычной команды тоже работает, но при плохой связи рвёт туннель на любой
// секундной заминке.
const (
	DefaultAliveInterval = 5
	DefaultAliveCountMax = 3
)

// Default — содержимое для первого запуска.
func Default() File {
	return File{
		Settings: Settings{ServerAliveInterval: DefaultAliveInterval, ServerAliveCountMax: DefaultAliveCountMax},
		Tunnels:  []Tunnel{},
	}
}

// DefaultPath — ~/.config/hotpipe/tunnels.json (или $XDG_CONFIG_HOME).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("не определить каталог настроек: %w", err)
	}
	return filepath.Join(dir, "hotpipe", "tunnels.json"), nil
}

// Load читает файл. Отсутствие файла — не ошибка, а первый запуск.
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return File{}, fmt.Errorf("не прочитать %s: %w", path, err)
	}
	f := Default()
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("не разобрать %s: %w", path, err)
	}
	if f.Tunnels == nil {
		f.Tunnels = []Tunnel{}
	}
	for i := range f.Tunnels {
		f.Tunnels[i].Normalize()
		if f.Tunnels[i].ID == "" {
			f.Tunnels[i].ID = NewID()
		}
	}
	if err := f.Validate(); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Save записывает файл целиком через временный и переименование: оборванная
// запись не должна оставить вместо настроек половину файла. Прежняя версия
// остаётся рядом как .bak — файл может править и человек.
func Save(path string, f File) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("не создать каталог настроек: %w", err)
	}
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("не записать %s: %w", tmp, err)
	}
	if old, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", old, 0o600)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("не заменить %s: %w", path, err)
	}
	return nil
}

// NewID — короткий случайный идентификатор туннеля.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Normalize подставляет умолчания в незаполненные поля.
func (t *Tunnel) Normalize() {
	t.Name = strings.TrimSpace(t.Name)
	t.Host = strings.TrimSpace(t.Host)
	t.User = strings.TrimSpace(t.User)
	t.LocalHost = strings.TrimSpace(t.LocalHost)
	t.RemoteHost = strings.TrimSpace(t.RemoteHost)
	t.IdentityFile = strings.TrimSpace(t.IdentityFile)
	if t.Port == 0 {
		t.Port = 22
	}
	if t.LocalHost == "" {
		t.LocalHost = "localhost"
	}
	if t.RemoteHost == "" {
		t.RemoteHost = "127.0.0.1"
	}
	if t.Name == "" && t.Host != "" {
		t.Name = fmt.Sprintf("%s:%d", t.Host, t.LocalPort)
	}
}

var (
	// Имя хоста, IPv4 или IPv6. Без пробелов, кавычек и прочего, что ssh
	// мог бы понять как что-то ещё; минус в начале запрещён отдельно.
	reHost = regexp.MustCompile(`^[A-Za-z0-9._:%\[\]-]{1,253}$`)
	reUser = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._@-]{0,63}$`)
)

func checkHost(what, h string) error {
	if h == "" {
		return fmt.Errorf("%s не указан", what)
	}
	if strings.HasPrefix(h, "-") || !reHost.MatchString(h) {
		return fmt.Errorf("%s %q: допустимы буквы, цифры и . - : [ ]", what, h)
	}
	return nil
}

func checkPort(what string, p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("%s: %d — вне 1…65535", what, p)
	}
	return nil
}

// Validate проверяет один туннель.
func (t Tunnel) Validate() error {
	if t.ID == "" {
		return errors.New("у туннеля нет идентификатора")
	}
	if len([]rune(t.Name)) > 80 {
		return errors.New("имя длиннее 80 знаков")
	}
	if err := checkHost("ssh-сервер", t.Host); err != nil {
		return err
	}
	if err := checkPort("порт ssh", t.Port); err != nil {
		return err
	}
	if t.User != "" && !reUser.MatchString(t.User) {
		return fmt.Errorf("пользователь %q: допустимы буквы, цифры и . _ - @", t.User)
	}
	if t.LocalHost != "localhost" && net.ParseIP(strings.Trim(t.LocalHost, "[]")) == nil {
		return fmt.Errorf("локальный адрес %q: нужен localhost или IP-адрес", t.LocalHost)
	}
	if err := checkPort("локальный порт", t.LocalPort); err != nil {
		return err
	}
	if err := checkHost("удалённый адрес", t.RemoteHost); err != nil {
		return err
	}
	if err := checkPort("удалённый порт", t.RemotePort); err != nil {
		return err
	}
	if t.IdentityFile != "" && (strings.HasPrefix(t.IdentityFile, "-") || strings.ContainsAny(t.IdentityFile, "\x00\n\r")) {
		return fmt.Errorf("путь к ключу %q недопустим", t.IdentityFile)
	}
	return nil
}

// Validate проверяет файл целиком: каждый туннель и то, что два туннеля не
// претендуют на один локальный порт — второй всё равно не поднялся бы.
func (f File) Validate() error {
	if f.Settings.ServerAliveInterval < 1 || f.Settings.ServerAliveInterval > 300 {
		return fmt.Errorf("server_alive_interval: %d — вне 1…300", f.Settings.ServerAliveInterval)
	}
	if f.Settings.ServerAliveCountMax < 1 || f.Settings.ServerAliveCountMax > 100 {
		return fmt.Errorf("server_alive_count_max: %d — вне 1…100", f.Settings.ServerAliveCountMax)
	}
	ids := map[string]bool{}
	ports := map[int]string{}
	for _, t := range f.Tunnels {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("туннель %q: %w", t.Name, err)
		}
		if ids[t.ID] {
			return fmt.Errorf("идентификатор %s повторяется", t.ID)
		}
		ids[t.ID] = true
		// Сравниваем по порту, не по паре адрес+порт: localhost занимает и
		// 127.0.0.1, и ::1, и пересечение с конкретным адресом легко пропустить.
		if other, ok := ports[t.LocalPort]; ok {
			return fmt.Errorf("локальный порт %d уже занят туннелем %q", t.LocalPort, other)
		}
		ports[t.LocalPort] = t.Name
	}
	return nil
}

// Find возвращает индекс туннеля по идентификатору или -1.
func (f File) Find(id string) int {
	for i, t := range f.Tunnels {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// Clone — глубокая копия: правки идут в копии, и при неудачной проверке
// действующие настройки не задеты.
func (f File) Clone() File {
	c := f
	c.Tunnels = append([]Tunnel(nil), f.Tunnels...)
	return c
}
