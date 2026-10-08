// Package tunnel запускает ssh, следит за ним и поднимает заново, когда он
// падает.
//
// На каждый включённый туннель — свой надзиратель (runner) со своим ssh.
// Manager сводит их с настройками: Apply получает файл целиком и сам решает,
// что запустить, что остановить, а что перезапустить из-за изменившихся
// параметров.
package tunnel

import (
	"slices"
	"sync"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

// Manager — все туннели сервера.
type Manager struct {
	ssh string

	mu      sync.Mutex
	order   []config.Tunnel
	runners map[string]*runner
}

// NewManager; ssh — путь к клиенту ssh.
func NewManager(ssh string) *Manager {
	return &Manager{ssh: ssh, runners: map[string]*runner{}}
}

// Apply приводит работающие туннели в соответствие с настройками.
//
// Туннель, параметры которого не изменились, не трогается: правка одного
// туннеля не должна рвать соединения через остальные.
func (m *Manager) Apply(f config.File) {
	m.mu.Lock()
	defer m.mu.Unlock()

	want := map[string]config.Tunnel{}
	for _, t := range f.Tunnels {
		want[t.ID] = t
	}

	// Сначала останавливаем лишнее и изменившееся — и только потом запускаем:
	// новый ssh мог бы претендовать на порт, который ещё держит старый.
	for id, r := range m.runners {
		t, ok := want[id]
		if !ok || !t.Enabled || !slices.Equal(r.args, Args(t, f.Settings)) {
			r.stop()
			delete(m.runners, id)
		}
	}
	for _, t := range f.Tunnels {
		if !t.Enabled {
			continue
		}
		if r, ok := m.runners[t.ID]; ok {
			// Аргументы те же, но имя могло поменяться.
			r.mu.Lock()
			r.st.Tunnel = t
			r.mu.Unlock()
			continue
		}
		r := newRunner(m.ssh, t, Args(t, f.Settings))
		m.runners[t.ID] = r
		r.start()
	}
	m.order = append([]config.Tunnel(nil), f.Tunnels...)
}

// Restart поднимает туннель заново: убивает нынешний ssh, если он есть, и
// сразу пробует снова, не дожидаясь очередного срока повтора.
func (m *Manager) Restart(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runners[id]
	if !ok {
		return false
	}
	r.stop()
	nr := newRunner(m.ssh, r.status().Tunnel, r.args)
	m.runners[id] = nr
	nr.start()
	return true
}

// RetryNow прерывает ожидание повтора у всех туннелей, ждущих его. Нужен,
// например, после того как вернулась сеть: ждать минуту незачем.
func (m *Manager) RetryNow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runners {
		r.retryNow()
	}
}

// Statuses — состояние всех туннелей в порядке файла настроек.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.order))
	for _, t := range m.order {
		if r, ok := m.runners[t.ID]; ok {
			out = append(out, r.status())
			continue
		}
		out = append(out, Status{Tunnel: t, State: Stopped, Output: []string{}})
	}
	return out
}

// StopAll останавливает все туннели — при завершении сервера.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var wg sync.WaitGroup
	for id, r := range m.runners {
		wg.Add(1)
		go func() { defer wg.Done(); r.stop() }()
		delete(m.runners, id)
	}
	wg.Wait()
}
