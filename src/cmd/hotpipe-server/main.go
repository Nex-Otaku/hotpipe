// hotpipe-server — демон постоянных SSH-туннелей.
//
// Поднимает туннели из ~/.config/hotpipe/tunnels.json сразу при запуске,
// следит за каждым ssh и поднимает его заново, когда тот падает. Управляется по
// HTTP на 127.0.0.1:46310 — окном hotpipe-gui или curl.
//
// Коды выхода:
//
//	0 — остановлен по просьбе (SIGTERM, SIGINT, POST /api/shutdown)
//	1 — не запустился
//	3 — порт API занят: скорее всего, сервер уже работает
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Nex-Otaku/hotpipe/internal/api"
	"github.com/Nex-Otaku/hotpipe/internal/config"
	"github.com/Nex-Otaku/hotpipe/internal/logfile"
	"github.com/Nex-Otaku/hotpipe/internal/tunnel"
	"github.com/Nex-Otaku/hotpipe/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		cfgPath  = flag.String("config", "", "файл туннелей (по умолчанию ~/.config/hotpipe/tunnels.json)")
		listen   = flag.String("listen", fmt.Sprintf("127.0.0.1:%d", api.DefaultPort), "адрес API")
		logPath  = flag.String("log", "", "журнал (по умолчанию hotpipe-server.log рядом с файлом туннелей)")
		noLog    = flag.Bool("no-log", false, "не вести журнал в файле")
		sshPath  = flag.String("ssh", "ssh", "клиент ssh")
		showVers = flag.Bool("version", false, "показать версию и выйти")
	)
	flag.Parse()

	if *showVers {
		fmt.Println(version.String("hotpipe-server"))
		return 0
	}

	if *cfgPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		*cfgPath = p
	}
	// Журнал — рядом с файлом туннелей, а не рядом с программой: программа
	// лежит в ~/.local/bin, и журнал там был бы чужим. Всё своё сервер держит в
	// одном каталоге — найти, перенести, снести.
	if err := os.MkdirAll(filepath.Dir(*cfgPath), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "не создать каталог настроек:", err)
		return 1
	}
	if *logPath == "" && !*noLog {
		*logPath = filepath.Join(filepath.Dir(*cfgPath), "hotpipe-server.log")
	}
	lf, err := logfile.Setup(*logPath, *noLog)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if lf != nil {
		defer lf.Close()
	} else {
		*logPath = ""
	}
	log.Print(version.String("hotpipe-server"))

	ssh, err := exec.LookPath(*sshPath)
	if err != nil {
		log.Printf("не найден клиент ssh (%s): %v", *sshPath, err)
		return 1
	}

	// Порт API занимаем прежде, чем поднимать туннели. Занят — значит, сервер
	// уже работает, и второй экземпляр поднял бы вторую пару ssh на те же порты.
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Printf("не занять %s: %v — сервер уже работает?", *listen, err)
		if errors.Is(err, syscall.EADDRINUSE) {
			return 3
		}
		return 1
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		// Испорченный файл — не повод молча начать с пустого и затереть его
		// при первой же правке из окна.
		log.Print(err)
		return 1
	}
	log.Printf("настройки: %s, туннелей %d", *cfgPath, len(cfg.Tunnels))

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	mgr := tunnel.NewManager(ssh)
	srv := api.New(*cfgPath, *logPath, cfg, mgr, stop)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for sig := range sigs {
			if sig == syscall.SIGHUP {
				if err := srv.Reload(); err != nil {
					log.Printf("SIGHUP: настройки не перечитаны: %v", err)
				} else {
					log.Print("SIGHUP: настройки перечитаны")
				}
				continue
			}
			log.Printf("получен %s — завершаюсь", sig)
			stop()
			return
		}
	}()

	log.Printf("API: http://%s", ln.Addr())
	if err := api.Serve(ctx, ln, srv.Handler()); err != nil {
		log.Printf("API: %v", err)
		mgr.StopAll()
		return 1
	}
	mgr.StopAll()
	log.Print("остановлен")
	return 0
}
