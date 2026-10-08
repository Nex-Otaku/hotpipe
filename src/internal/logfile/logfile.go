// Package logfile — журнал процесса в файле рядом с самой программой.
//
// Рядом с двоичным файлом, а не в системном месте для журналов: hotpipe-server
// wsldrive переносят папкой целиком, и журнал должен переезжать вместе с ними.
// Искать его тогда не нужно — он лежит там же, откуда запущена программа.
//
// Журнал ведётся по умолчанию, и это решение, а не умолчание по недосмотру.
// Однажды затор синхронизации пришлось разгадывать по одним счётчикам
// состояния: окно управления запускало обе стороны с отключённым выводом, и
// сообщения об ошибках уходили в никуда (Р-043). Чтобы такое не повторилось,
// молчание теперь надо запрашивать явно.
package logfile

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MaxSize — при каком размере журнал откладывается в сторону.
//
// Предел нужен: туннель, который не может подняться, повторяет попытки
// сутками, и каждая оставляет в журнале строки ssh. Откладывается ровно
// один прошлый файл — этого хватает, чтобы застать начало неприятности, а
// хранить историю за месяцы здесь незачем.
const MaxSize int64 = 16 << 20

// Log — файл журнала, сам за собой следящий по размеру.
type Log struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// DefaultPath возвращает путь журнала рядом с самой программой: то же имя, но
// с расширением .log.
func DefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("не определить путь программы: %w", err)
	}
	// Символические ссылки разворачиваются: журнал должен лечь рядом с самим
	// файлом, а не рядом со ссылкой на него.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	base := filepath.Base(exe)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return filepath.Join(filepath.Dir(exe), base+".log"), nil
}

// Open открывает журнал по указанному пути; пустой путь означает «рядом с
// программой».
//
// Возможность записи проверяется здесь же, самим открытием файла: узнать, что
// журнал вести некуда, надо при запуске, а не в ту минуту, когда в него
// понадобится что-то записать.
func Open(path string) (*Log, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}
	l := &Log{path: path, max: MaxSize}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Log) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("не открыть журнал %s: %w", l.path, err)
	}
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return fmt.Errorf("не определить размер журнала %s: %w", l.path, err)
	}
	l.f, l.size = f, size
	return nil
}

// Path сообщает, куда пишется журнал.
func (l *Log) Path() string { return l.path }

func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.max > 0 && l.size+int64(len(p)) > l.max {
		l.rotate()
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

// rotate откладывает заполненный журнал и начинает новый. Неудача отката не
// прерывает работу: журнал — средство разбирательства, а не сама работа, и
// останавливать из-за него туннели было бы хуже, чем писать в разросшийся файл.
func (l *Log) rotate() {
	l.f.Close()
	l.f = nil
	prev := l.path + ".1"
	os.Remove(prev)
	renameErr := os.Rename(l.path, prev)
	if err := l.open(); err != nil {
		return
	}
	if renameErr != nil {
		// Не переименовалось — пишем дальше в тот же файл: потерять сообщения
		// хуже, чем перебрать предел.
		fmt.Fprintf(l.f, "не отложить прежний журнал: %v\n", renameErr)
	}
}

func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Setup направляет стандартный журнал в файл и возвращает открытый журнал.
//
// Пишем и в файл, и в стандартный поток ошибок. Запущенная из окна управления
// программа консоли не имеет, и без файла её сообщения пропадают; запущенная
// же руками из терминала должна отвечать человеку сразу, а не после того, как
// он найдёт файл.
//
// disabled отключает файл целиком — единственный случай, когда журнал не
// ведётся.
func Setup(path string, disabled bool) (*Log, error) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if disabled {
		return nil, nil
	}
	l, err := Open(path)
	if err != nil {
		return nil, err
	}
	log.SetOutput(io.MultiWriter(os.Stderr, l))
	return l, nil
}
