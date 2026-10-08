package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func valid() Tunnel {
	t := Tunnel{ID: "a1", Host: "203.0.113.10", Port: 2222, User: "deploy",
		LocalPort: 5001, RemotePort: 3306, Enabled: true}
	t.Normalize()
	return t
}

func TestNormalizeDefaults(t *testing.T) {
	tn := Tunnel{Host: " h ", LocalPort: 1, RemotePort: 2}
	tn.Normalize()
	if tn.Port != 22 || tn.LocalHost != "localhost" || tn.RemoteHost != "127.0.0.1" || tn.Host != "h" || tn.Name != "h:1" {
		t.Fatalf("умолчания не подставлены: %+v", tn)
	}
}

// Всё, что попадает в командную строку ssh, не может начинаться с минуса и
// содержать пробелы: иначе через API можно было бы передать ssh ключ вроде
// -oProxyCommand и выполнить произвольную команду.
func TestValidateRejectsInjection(t *testing.T) {
	cases := map[string]func(*Tunnel){
		"host-dash":     func(t *Tunnel) { t.Host = "-oProxyCommand=touch /tmp/x" },
		"host-space":    func(t *Tunnel) { t.Host = "a b" },
		"remote-dash":   func(t *Tunnel) { t.RemoteHost = "-x" },
		"user-dash":     func(t *Tunnel) { t.User = "-l" },
		"user-space":    func(t *Tunnel) { t.User = "a b" },
		"identity-dash": func(t *Tunnel) { t.IdentityFile = "-oFoo" },
		"identity-nl":   func(t *Tunnel) { t.IdentityFile = "a\nb" },
		"local-host":    func(t *Tunnel) { t.LocalHost = "0.0.0.0 -R" },
		"port-zero":     func(t *Tunnel) { t.LocalPort = 0 },
		"port-big":      func(t *Tunnel) { t.RemotePort = 70000 },
		"no-host":       func(t *Tunnel) { t.Host = "" },
	}
	for name, mut := range cases {
		tn := valid()
		mut(&tn)
		if err := tn.Validate(); err == nil {
			t.Errorf("%s: принято %+v", name, tn)
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("годный отвергнут: %v", err)
	}
	ok := valid()
	ok.Host, ok.RemoteHost, ok.LocalHost = "::1", "db.internal", "127.0.0.1"
	if err := ok.Validate(); err != nil {
		t.Fatalf("IPv6 и имя отвергнуты: %v", err)
	}
}

func TestValidateDuplicatePort(t *testing.T) {
	f := Default()
	a, b := valid(), valid()
	b.ID = "b2"
	f.Tunnels = []Tunnel{a, b}
	if err := f.Validate(); err == nil || !strings.Contains(err.Error(), "5001") {
		t.Fatalf("два туннеля на одном порту приняты: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "tunnels.json")
	f, err := Load(path)
	if err != nil || len(f.Tunnels) != 0 {
		t.Fatalf("отсутствующий файл: %v %+v", err, f)
	}
	f.Tunnels = append(f.Tunnels, valid())
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	f.Tunnels[0].Name = "второй"
	if err := Save(path, f); err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.Tunnels[0].Name != "второй" || g.Settings.ServerAliveInterval != DefaultAliveInterval {
		t.Fatalf("прочитано не то: %+v", g)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("нет копии прежней версии: %v", err)
	}
}

// Испорченный файл — ошибка, а не пустые настройки: иначе первая же правка
// из окна затёрла бы его.
func TestLoadBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnels.json")
	os.WriteFile(path, []byte("{oops"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("испорченный файл принят")
	}
}

// Туннель без идентификатора (написанный руками) получает его при чтении.
func TestLoadAssignsID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnels.json")
	os.WriteFile(path, []byte(`{"tunnels":[{"host":"h","local_port":1,"remote_port":2}]}`), 0o600)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Tunnels[0].ID == "" || f.Settings.ServerAliveCountMax != DefaultAliveCountMax {
		t.Fatalf("%+v", f)
	}
}
