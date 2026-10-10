package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Невозможность писать журнал должна выясняться при открытии, а не в ту минуту,
// когда в журнал понадобится что-то записать.
func TestOpenReportsUnwritablePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("права каталога не меняются:", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if os.Geteuid() == 0 {
		t.Skip("под root права каталога не помеха")
	}
	if _, err := Open(filepath.Join(dir, "нельзя.log")); err == nil {
		t.Fatal("открытие в закрытом на запись каталоге должно было не удаться")
	}
}

func TestWriteAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("первая\n"))
	l.Close()

	l, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("вторая\n"))
	l.Close()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, "первая") || !strings.Contains(got, "вторая") {
		t.Fatalf("прежние записи потеряны: %q", got)
	}
}

// Заполненный журнал откладывается в сторону, а не растёт без предела: в заторе
// сообщения шли по десятку в секунду.
func TestRotateKeepsPrevious(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.max = 100

	for i := 0; i < 20; i++ {
		l.Write([]byte(strings.Repeat("a", 20) + "\n"))
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("прежний журнал не отложен: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() > 100 {
		t.Fatalf("журнал перерос предел: %d Б", fi.Size())
	}
}

// Журнал ложится рядом с самой программой: программу переносят папкой
// целиком, и искать его тогда не нужно.
func TestDefaultPathSitsBesideBinary(t *testing.T) {
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	// DefaultPath разворачивает ссылки; в macOS ссылка — сам /var (→ /private/var).
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if filepath.Dir(p) != filepath.Dir(exe) {
		t.Errorf("журнал %s не рядом с программой %s", p, exe)
	}
	if filepath.Ext(p) != ".log" {
		t.Errorf("неожиданное расширение: %s", p)
	}
}
