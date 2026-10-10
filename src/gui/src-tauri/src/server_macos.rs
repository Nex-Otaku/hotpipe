// Запуск сервера в macOS: его держит launchd (Р-010).
//
// В Windows серверу нужен сторож — WSL убивает всё, что запущено через
// wsl.exe, когда тот выходит. В macOS сервер — обычный процесс, а «держать и
// поднимать заново» launchd умеет сам: задание пользователя (LaunchAgent) с
// KeepAlive поднимает упавший сервер, с RunAtLoad — запускает его при входе.
// Поэтому «сторож» здесь — это задание launchd, а не наш процесс:
//
//   сторож работает   — задание загружено в launchd (launchctl print);
//   поставить сторожа — записать описание задания и загрузить (bootstrap);
//   убрать сторожа    — выгрузить (bootout) и запретить (disable), чтобы
//                       выключенный сервер не поднялся и при следующем входе.
//
// Описание задания лежит в каталоге окна; автозапуск (autostart_macos.rs) —
// это та же запись, положенная ещё и в ~/Library/LaunchAgents.

use crate::applog;
use crate::config::{self, Config};
use crate::http;
use std::path::PathBuf;
use std::process::Command;

pub const LABEL: &str = "dev.hotpipe.server";

/// Отвечает ли сервер.
pub fn alive(cfg: &Config) -> bool {
    matches!(http::request(cfg.api_port, "GET", "/api/status", None), Ok(r) if r.code == 200)
}

fn uid() -> String {
    Command::new("/usr/bin/id")
        .arg("-u")
        .output()
        .ok()
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_default()
}

fn domain() -> String {
    format!("gui/{}", uid())
}

fn service() -> String {
    format!("{}/{LABEL}", domain())
}

fn launchctl(args: &[&str]) -> Result<String, String> {
    let out = Command::new("/bin/launchctl")
        .args(args)
        .output()
        .map_err(|e| format!("не запустить launchctl: {e}"))?;
    if out.status.success() {
        return Ok(String::from_utf8_lossy(&out.stdout).into_owned());
    }
    let mut msg = String::from_utf8_lossy(&out.stderr).trim().to_string();
    if msg.is_empty() {
        msg = String::from_utf8_lossy(&out.stdout).trim().to_string();
    }
    Err(format!("launchctl {}: {msg}", args.join(" ")))
}

/// Путь к hotpipe-server. Пусто в настройках — сначала тот, что лежит в
/// HotPipe.app рядом с окном, затем ~/.local/bin, куда кладёт build/install.sh.
pub fn server_binary(cfg: &Config) -> PathBuf {
    let set = cfg.server_binary.trim();
    if !set.is_empty() {
        return PathBuf::from(set);
    }
    if let Some(dir) = std::env::current_exe().ok().and_then(|p| p.parent().map(|d| d.to_path_buf())) {
        let bundled = dir.join("hotpipe-server");
        if bundled.is_file() {
            return bundled;
        }
    }
    config::home().join(".local/bin/hotpipe-server")
}

fn xml_escape(s: &str) -> String {
    s.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;")
}

/// Описание задания launchd.
///
/// PATH задаём сами: launchd даёт заданиям только /usr/bin:/bin:/usr/sbin:/sbin,
/// а ProxyCommand из ~/.ssh/config часто зовёт программы из Homebrew.
pub fn plist(cfg: &Config) -> String {
    let bin = xml_escape(&server_binary(cfg).display().to_string());
    let listen = format!("127.0.0.1:{}", cfg.api_port);
    let out = xml_escape(&config::app_dir().join("hotpipe-server.stderr.log").display().to_string());
    format!(
        r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{LABEL}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{bin}</string>
		<string>-listen</string>
		<string>{listen}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
	<key>StandardOutPath</key>
	<string>{out}</string>
	<key>StandardErrorPath</key>
	<string>{out}</string>
</dict>
</plist>
"#
    )
}

/// Описание задания в каталоге окна — его загружаем, когда автозапуск выключен.
pub fn job_path() -> PathBuf {
    config::app_dir().join(format!("{LABEL}.plist"))
}

/// Загружено ли задание в launchd.
pub fn keeper_alive() -> bool {
    launchctl(&["print", &service()]).is_ok()
}

/// Записывает описание задания и загружает его. Запрет (disable), оставшийся
/// от выключения сервера, снимается: человек сам попросил включить.
pub fn spawn_keeper() -> Result<(), String> {
    let (cfg, _) = config::load();
    let bin = server_binary(&cfg);
    if !bin.is_file() {
        return Err(format!(
            "не найден hotpipe-server: {} — соберите его (build/build.sh, build/install.sh) или укажите путь в настройках",
            bin.display()
        ));
    }
    let body = plist(&cfg);
    let path = job_path();
    std::fs::write(&path, &body).map_err(|e| format!("не записать {}: {e}", path.display()))?;
    // Автозапуск должен вести к тому же серверу, что и задание сейчас.
    crate::autostart::refresh(&body)?;
    let _ = launchctl(&["enable", &service()]);
    if keeper_alive() {
        // Задание уже загружено: kickstart запустит сервер, если тот не
        // работает (launchd мог отложить перезапуск после частых падений).
        return launchctl(&["kickstart", &service()]).map(|_| ());
    }
    applog::line("окно", &format!("загружаю задание launchd {}", path.display()));
    launchctl(&["bootstrap", &domain(), &path.display().to_string()]).map(|_| ())
}

/// Выгружает задание — launchd завершит сервер (SIGTERM), и сервер сам
/// остановит свои ssh, — и запрещает его: иначе при следующем входе автозапуск
/// поднял бы выключенный сервер.
pub fn stop_keeper() -> Result<(), String> {
    let _ = launchctl(&["disable", &service()]);
    if keeper_alive() {
        launchctl(&["bootout", &service()])?;
    }
    Ok(())
}

/// В macOS сторож — launchd, отдельного режима у окна нет. Ключ --keeper
/// оставлен, чтобы старые ярлыки ничего не ломали: он просто ставит задание.
pub fn keeper() {
    if let Err(e) = spawn_keeper() {
        applog::line("сторож", &e);
    }
}

/// Дистрибутивов WSL в macOS нет.
pub fn distros() -> Vec<String> {
    vec![]
}
