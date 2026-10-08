package tunnel

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Сообщения ssh переводятся в понятные слова с подсказкой, что делать.
// Сырые строки остаются в выводе туннеля и в журнале — перевод их не заменяет,
// а избавляет от необходимости читать их в каждом обычном случае.
var diagnoses = []struct {
	needles []string
	text    string
}{
	{[]string{"permission denied"},
		"сервер не принял ключ. Демону нужен вход по ключу без пароля и без вопросов: ssh-copy-id -p <порт> <пользователь>@<сервер>"},
	{[]string{"host key verification failed", "remote host identification has changed"},
		"ключ сервера не совпал с запомненным в ~/.ssh/known_hosts. Если сервер переустанавливали — уберите старую запись: ssh-keygen -R <сервер>"},
	{[]string{"could not resolve hostname", "name or service not known", "temporary failure in name resolution"},
		"имя сервера не находится — нет сети или ошибка в имени"},
	{[]string{"connection refused"},
		"сервер отказал в подключении: на этом порту ssh не слушает"},
	{[]string{"network is unreachable", "no route to host"},
		"нет сети до сервера"},
	{[]string{"connection timed out", "operation timed out"},
		"сервер не ответил — нет сети или порт закрыт"},
	{[]string{"address already in use", "cannot listen to port", "could not request local forwarding"},
		"локальный порт занят другим процессом"},
	{[]string{"timeout, server", "not responding"},
		"связь потеряна: сервер перестал отвечать на проверки"},
	{[]string{"bad owner or permissions"},
		"ssh отверг файл с неверными правами (ключ или ~/.ssh/config): chmod 600"},
	{[]string{"no such identity", "not accessible"},
		"файл ключа не найден"},
	{[]string{"connection closed by", "connection reset", "broken pipe", "kex_exchange_identification"},
		"сервер закрыл соединение"},
}

// Diagnose объясняет, почему ssh завершился, по его последним строкам и коду
// выхода.
func Diagnose(lines []string, err error) string {
	// Смотрим с конца: последняя причина — главная. Первые строки часто
	// предупреждения вроде «Warning: Permanently added …», к делу не относящиеся.
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.ToLower(lines[i])
		for _, d := range diagnoses {
			for _, n := range d.needles {
				if strings.Contains(l, n) {
					return d.text
				}
			}
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "Warning:") {
			return "ssh: " + l
		}
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() >= 0 {
			return fmt.Sprintf("ssh завершился с кодом %d", ee.ExitCode())
		}
		return "ssh завершён сигналом: " + ee.String()
	}
	if err != nil {
		return "ssh завершился: " + err.Error()
	}
	// Код 0 без единой строки — ssh не сломался, его завершили: сигналом
	// снаружи или закрытием соединения со стороны сервера.
	return "ssh завершился без ошибки — его завершили снаружи или сервер закрыл соединение"
}
