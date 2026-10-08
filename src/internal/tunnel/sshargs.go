package tunnel

import (
	"strconv"
	"strings"

	"github.com/Nex-Otaku/hotpipe/internal/config"
)

// Args — аргументы ssh для туннеля.
//
// Отличия от привычной команды «ssh -f -N -L …» и почему они нужны:
//
//   - без -f. ssh остаётся нашим дочерним процессом: мы видим его выход, его
//     вывод и знаем его pid. С -f он уходил бы в фон, и следить было бы не за чем;
//   - BatchMode=yes. Спросить пароль или подтверждение некого — без этого ssh
//     молча ждал бы ввода вечно, и туннель висел бы в «подключаюсь»;
//   - ExitOnForwardFailure=yes. Если локальный порт занят, ssh без этого
//     остаётся работать без перенаправления — живой процесс, мёртвый туннель;
//   - ControlMaster=no, ControlPath=none. Общее соединение из ~/.ssh/config
//     сделало бы наш ssh клиентом чужого процесса, и его выход ничего бы не
//     значил;
//   - StrictHostKeyChecking=accept-new. Новый хост запоминается сам — спросить
//     некого, — а подмена ключа у известного по-прежнему отвергается.
func Args(t config.Tunnel, s config.Settings) []string {
	a := []string{
		"-N", "-T",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=" + strconv.Itoa(s.ServerAliveInterval),
		"-o", "ServerAliveCountMax=" + strconv.Itoa(s.ServerAliveCountMax),
		"-o", "ConnectTimeout=15",
		"-o", "ControlMaster=no",
		"-o", "ControlPath=none",
		"-o", "StrictHostKeyChecking=accept-new",
		"-p", strconv.Itoa(t.Port),
	}
	if t.IdentityFile != "" {
		a = append(a, "-i", t.IdentityFile, "-o", "IdentitiesOnly=yes")
	}
	a = append(a, "-L", bracket(t.LocalHost)+":"+strconv.Itoa(t.LocalPort)+":"+
		bracket(t.RemoteHost)+":"+strconv.Itoa(t.RemotePort))
	if t.User != "" {
		a = append(a, "-l", t.User)
	}
	// «--» — последняя защита: что бы ни оказалось в имени хоста, ключом ssh
	// его уже не поймёт.
	return append(a, "--", t.Host)
}

// bracket заключает адрес IPv6 в скобки, как того требует -L.
func bracket(h string) string {
	if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
		return "[" + h + "]"
	}
	return h
}
