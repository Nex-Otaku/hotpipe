// Запуск сервера в WSL и сторож, который его держит.
//
// Главное обстоятельство, выясненное ещё в wsldrive: WSL убивает всё, что
// запущено вызовом wsl.exe, когда тот выходит, — даже отвязанное через setsid и
// nohup. Поэтому сервер нельзя «запустить и забыть»: кто-то в Windows должен
// держать открытым wsl.exe, внутри которого он работает. Этим занят сторож —
// тот же hotpipe-gui.exe с ключом --keeper, без окна (Р-002).

use crate::applog;
use crate::config::{self, Config};
use crate::http;
use std::fs::{File, OpenOptions};
use std::os::windows::fs::OpenOptionsExt;
use std::os::windows::process::CommandExt;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

pub const CREATE_NO_WINDOW: u32 = 0x0800_0000;
const DETACHED_PROCESS: u32 = 0x0000_0008;
const CREATE_NEW_PROCESS_GROUP: u32 = 0x0000_0200;

/// Отвечает ли сервер.
pub fn alive(cfg: &Config) -> bool {
    matches!(http::request(cfg.api_port, "GET", "/api/status", None), Ok(r) if r.code == 200)
}

/// wsl.exe с аргументами, запускающими сервер.
pub fn wsl_command(cfg: &Config) -> Command {
    let mut c = Command::new("wsl.exe");
    if !cfg.distro.trim().is_empty() {
        c.args(["-d", cfg.distro.trim()]);
    }
    let bin = cfg.server_binary.trim();
    if bin.is_empty() {
        // Путь по умолчанию — в домашнем каталоге, а его имени отсюда не
        // знать. «--cd ~» делает домашний каталог текущим, и относительный путь
        // находится без оболочки и её кавычек.
        c.args(["--cd", "~", "-e", "./.local/bin/hotpipe-server"]);
    } else {
        c.args(["-e", bin]);
    }
    c.args(["-listen", &format!("127.0.0.1:{}", cfg.api_port)]);
    c.creation_flags(CREATE_NO_WINDOW)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    c
}

// --- сторож ------------------------------------------------------------------

fn lock_path() -> std::path::PathBuf {
    config::app_dir().join("keeper.lock")
}

/// Открывает файл замка без совместного доступа: пока сторож жив, второй
/// открыть его не сможет. Замок снимает сама Windows, когда процесс уходит, —
/// даже убитый, — поэтому застрявших замков не бывает.
fn take_lock() -> Option<File> {
    OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(false)
        .share_mode(0)
        .open(lock_path())
        .ok()
}

/// Работает ли сторож.
pub fn keeper_alive() -> bool {
    match OpenOptions::new().write(true).create(true).truncate(false).share_mode(0).open(lock_path()) {
        Ok(_) => false, // открылся — значит, никто не держит (и тут же закрылся)
        Err(e) => e.raw_os_error() == Some(32), // ERROR_SHARING_VIOLATION
    }
}

/// Запускает сторожа отдельным процессом, открепившимся от окна: закрытие окна
/// не должно ронять туннели.
pub fn spawn_keeper() -> Result<(), String> {
    let exe = std::env::current_exe().map_err(|e| format!("не найти себя: {e}"))?;
    Command::new(exe)
        .arg("--keeper")
        .creation_flags(CREATE_NO_WINDOW | DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map(|_| ())
        .map_err(|e| format!("не запустить сторожа: {e}"))
}

/// Повторы после неудачного запуска: сервер, падающий сразу (нет файла,
/// испорчены настройки), незачем дёргать каждую секунду.
const BACKOFF: [u64; 5] = [2, 5, 10, 30, 60];

/// Главный цикл сторожа. Сервер поднимается, пока он нужен (server_enabled),
/// и поднимается снова, если упал или его убили, — в том числе после
/// wsl --shutdown.
pub fn keeper() {
    let Some(_lock) = take_lock() else {
        return; // другой сторож уже работает
    };
    applog::line("сторож", "запущен");
    let mut fails = 0usize;
    loop {
        let (cfg, _) = config::load();
        if !cfg.server_enabled {
            applog::line("сторож", "сервер выключен в настройках — ухожу");
            return;
        }
        // Сервер мог быть поднят и без нас — руками в WSL. Тогда просто
        // присматриваем.
        if alive(&cfg) {
            fails = 0;
            std::thread::sleep(Duration::from_secs(5));
            continue;
        }
        applog::line("сторож", "сервер не отвечает — запускаю");
        let started = Instant::now();
        match wsl_command(&cfg).spawn() {
            Ok(mut child) => match child.wait() {
                Ok(st) => applog::line(
                    "сторож",
                    &format!("сервер завершился: {st}, проработав {} с", started.elapsed().as_secs()),
                ),
                Err(e) => applog::line("сторож", &format!("ожидание wsl.exe: {e}")),
            },
            Err(e) => applog::line("сторож", &format!("не запустить wsl.exe: {e}")),
        }
        if started.elapsed() < Duration::from_secs(30) {
            fails += 1;
        } else {
            fails = 0;
        }
        let delay = BACKOFF[fails.min(BACKOFF.len() - 1)];
        // Ждём по секунде, сверяясь с настройками: выключение из окна должно
        // действовать сразу, а не после минутной паузы.
        for _ in 0..delay {
            std::thread::sleep(Duration::from_secs(1));
            if !config::load().0.server_enabled {
                break;
            }
        }
    }
}

// --- прочее ------------------------------------------------------------------

/// Дистрибутивы WSL — для выбора в настройках. wsl.exe отвечает в UTF-16.
pub fn distros() -> Vec<String> {
    let Ok(out) = Command::new("wsl.exe")
        .args(["-l", "-q"])
        .creation_flags(CREATE_NO_WINDOW)
        .output()
    else {
        return vec![];
    };
    let u16s: Vec<u16> = out
        .stdout
        .chunks_exact(2)
        .map(|c| u16::from_le_bytes([c[0], c[1]]))
        .collect();
    String::from_utf16_lossy(&u16s)
        .lines()
        .map(|l| l.trim().trim_matches('\0').to_string())
        .filter(|l| !l.is_empty())
        .collect()
}
