// hotpipe-gui — окно управления сервером SSH-туннелей HotPipe.
//
// Два режима в одном файле:
//
//   hotpipe-gui.exe            окно
//   hotpipe-gui.exe --keeper   сторож без окна: держит wsl.exe с сервером и
//                              поднимает сервер, если тот упал (server.rs)
//
// Окно своего учёта туннелей не ведёт: всё берётся у сервера по HTTP и туда
// же уходит. Два списка туннелей рано или поздно разошлись бы.

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod applog;
#[cfg_attr(target_os = "macos", path = "autostart_macos.rs")]
mod autostart;
mod config;
mod http;
// В macOS сервер держит launchd, а не сторож с wsl.exe (Р-010).
#[cfg_attr(target_os = "macos", path = "server_macos.rs")]
mod server;

use config::Config;
use serde::Serialize;
use std::sync::Mutex;
use std::time::{Duration, Instant};

const VERSION: &str = env!("HOTPIPE_VERSION");
const COMMIT: &str = env!("HOTPIPE_COMMIT");
const BUILT: &str = env!("HOTPIPE_BUILT");
const DIRTY: &str = env!("HOTPIPE_DIRTY");

fn build_line() -> String {
    let mut s = format!(
        "hotpipe-gui {VERSION} (снимок {COMMIT}, собрано {BUILT}, {}/{})",
        std::env::consts::OS,
        std::env::consts::ARCH
    );
    if DIRTY == "yes" {
        s.push_str(" [изменённое дерево]");
    }
    s
}

struct App {
    cfg: Mutex<Config>,
    notice: Mutex<Option<String>>,
    busy: Mutex<bool>,
}

/// Признак занятости на время включения и выключения сервера; снимается, чем
/// бы дело ни кончилось.
struct BusyGuard<'a>(&'a Mutex<bool>);

impl<'a> BusyGuard<'a> {
    fn new(m: &'a Mutex<bool>) -> Option<Self> {
        let mut b = m.lock().ok()?;
        if *b {
            return None;
        }
        *b = true;
        Some(BusyGuard(m))
    }
}

impl Drop for BusyGuard<'_> {
    fn drop(&mut self) {
        if let Ok(mut b) = self.0.lock() {
            *b = false;
        }
    }
}

#[derive(Serialize)]
struct Status {
    /// Ответ сервера /api/status как есть: новые поля появляются в окне сами.
    server: Option<serde_json::Value>,
    server_error: Option<String>,
    keeper: bool,
    autostart: autostart::State,
    busy: bool,
    config: Config,
    notice: Option<String>,
    version: String,
    build: String,
    /// «windows» или «macos» — окно показывает то, что есть на этой системе.
    platform: &'static str,
}

// Все команды — (async): без этого Tauri 1.x исполняет их в главном потоке, и
// окно не отвечает, пока команда не вернётся (урок wsldrive, Р-052 там).

#[tauri::command(async)]
fn get_status(app: tauri::State<App>) -> Status {
    let cfg = app.cfg.lock().unwrap().clone();
    let (server, server_error) = match http::request(cfg.api_port, "GET", "/api/status", None) {
        Ok(r) if r.code == 200 => match serde_json::from_str(&r.body) {
            Ok(v) => (Some(v), None),
            Err(e) => (None, Some(format!("ответ не разобран: {e}"))),
        },
        Ok(r) => (None, Some(format!("сервер ответил {}", r.code))),
        Err(e) => (None, Some(e)),
    };
    Status {
        server,
        server_error,
        keeper: server::keeper_alive(),
        autostart: autostart::get(),
        busy: *app.busy.lock().unwrap(),
        config: cfg,
        notice: app.notice.lock().unwrap().clone(),
        version: VERSION.to_string(),
        build: build_line(),
        platform: std::env::consts::OS,
    }
}

/// Запрос к API сервера. Ошибкой считается и ответ с кодом не из 2xx — его
/// текст сервер кладёт в поле error.
#[tauri::command(async)]
fn api(app: tauri::State<App>, method: String, path: String, body: Option<serde_json::Value>) -> Result<serde_json::Value, String> {
    if !path.starts_with("/api/") {
        return Err(format!("недопустимый путь {path}"));
    }
    let port = app.cfg.lock().unwrap().api_port;
    let body = body.map(|b| b.to_string());
    let r = http::request(port, &method, &path, body.as_deref())?;
    let v: serde_json::Value = serde_json::from_str(&r.body).unwrap_or(serde_json::Value::Null);
    if (200..300).contains(&r.code) {
        return Ok(v);
    }
    Err(v.get("error")
        .and_then(|e| e.as_str())
        .map(str::to_string)
        .unwrap_or_else(|| format!("сервер ответил {}", r.code)))
}

fn set_enabled(app: &App, on: bool) -> Result<Config, String> {
    let mut cfg = app.cfg.lock().unwrap();
    cfg.server_enabled = on;
    config::save(&cfg)?;
    Ok(cfg.clone())
}

fn wait_until(limit: Duration, mut ready: impl FnMut() -> bool) -> bool {
    let deadline = Instant::now() + limit;
    loop {
        if ready() {
            return true;
        }
        if Instant::now() >= deadline {
            return false;
        }
        std::thread::sleep(Duration::from_millis(300));
    }
}

/// Ставить ли сторожа. В macOS — всегда: spawn_keeper там ничего не трогает,
/// если задание уже загружено и не устарело, а устаревшее (HotPipe.app
/// переложили) загружает заново (Р-010).
fn must_spawn_keeper() -> bool {
    cfg!(target_os = "macos") || !server::keeper_alive()
}

/// Включить сервер: запомнить желание и поставить сторожа, если его нет.
#[tauri::command(async)]
fn server_on(app: tauri::State<App>) -> Result<(), String> {
    let _g = BusyGuard::new(&app.busy).ok_or("уже выполняется")?;
    let cfg = set_enabled(&app, true)?;
    applog::line("окно", "сервер включён");
    if must_spawn_keeper() {
        server::spawn_keeper()?;
    }
    // WSL после перезагрузки поднимается секунд десять, и это не сбой.
    if wait_until(Duration::from_secs(40), || server::alive(&cfg)) {
        return Ok(());
    }
    Err(format!("сервер не отозвался за 40 секунд — смотрите {}", applog::path().display()))
}

/// Выключить сервер: сначала запомнить желание — иначе сторож тут же поднял
/// бы его снова, — потом попросить завершиться.
#[tauri::command(async)]
fn server_off(app: tauri::State<App>) -> Result<(), String> {
    let _g = BusyGuard::new(&app.busy).ok_or("уже выполняется")?;
    let cfg = set_enabled(&app, false)?;
    applog::line("окно", "сервер выключен");
    // launchd с KeepAlive поднял бы завершившийся сервер — задание выгружаем.
    #[cfg(target_os = "macos")]
    server::stop_keeper()?;
    let _ = http::request(cfg.api_port, "POST", "/api/shutdown", None);
    if wait_until(Duration::from_secs(15), || !server::alive(&cfg)) {
        return Ok(());
    }
    Err("сервер всё ещё отвечает".into())
}

/// Перезапуск: сервер завершается, сторож поднимает его заново. Нужен после
/// обновления hotpipe-server.
#[tauri::command(async)]
fn server_restart(app: tauri::State<App>) -> Result<(), String> {
    let _g = BusyGuard::new(&app.busy).ok_or("уже выполняется")?;
    let cfg = set_enabled(&app, true)?;
    applog::line("окно", "перезапуск сервера");
    // В macOS задание выгружается и загружается заново: так launchd прочтёт
    // описание с новыми путём и портом из настроек.
    #[cfg(target_os = "macos")]
    server::stop_keeper()?;
    let _ = http::request(cfg.api_port, "POST", "/api/shutdown", None);
    wait_until(Duration::from_secs(10), || !server::alive(&cfg));
    if must_spawn_keeper() {
        server::spawn_keeper()?;
    }
    if wait_until(Duration::from_secs(40), || server::alive(&cfg)) {
        return Ok(());
    }
    Err("сервер не поднялся за 40 секунд".into())
}

#[tauri::command(async)]
fn set_autostart(on: bool) -> Result<(), String> {
    autostart::set(on)
}

#[tauri::command(async)]
fn save_settings(app: tauri::State<App>, cfg: Config) -> Result<(), String> {
    let mut cur = app.cfg.lock().unwrap();
    // Желание «сервер включён» переключается кнопкой, а не формой настроек.
    let next = Config { server_enabled: cur.server_enabled, ..cfg };
    config::save(&next)?;
    *cur = next;
    *app.notice.lock().unwrap() = None;
    Ok(())
}

#[tauri::command(async)]
fn list_distros() -> Vec<String> {
    server::distros()
}

#[tauri::command(async)]
fn gui_log() -> String {
    std::fs::read_to_string(applog::path()).unwrap_or_default()
}

fn main() {
    if std::env::args().any(|a| a == "--keeper") {
        server::keeper();
        return;
    }

    let (cfg, notice) = config::load();

    // Каталог данных WebView2 — рядом с программой, а не в %LOCALAPPDATA%:
    // программа переезжает папкой, и всё её должно ехать с ней (как в
    // wsldrive). Задать его можно только при постройке окна, поэтому окна
    // строим сами по описанию из tauri.conf.json.
    let mut context = tauri::generate_context!();
    let windows = std::mem::take(&mut context.config_mut().tauri.windows);
    let data_dir = config::app_dir().join("webview");

    // Открыв окно, человек ожидает работающий сервер: если он должен работать,
    // а сторожа нет — ставим. Выключенный кнопкой сервер не трогаем.
    if cfg.server_enabled && must_spawn_keeper() {
        if let Err(e) = server::spawn_keeper() {
            applog::line("окно", &e);
        }
    }

    tauri::Builder::default()
        .manage(App {
            cfg: Mutex::new(cfg),
            notice: Mutex::new(notice),
            busy: Mutex::new(false),
        })
        .invoke_handler(tauri::generate_handler![
            get_status,
            api,
            server_on,
            server_off,
            server_restart,
            set_autostart,
            save_settings,
            list_distros,
            gui_log
        ])
        .setup(move |app| {
            for w in windows {
                tauri::WindowBuilder::from_config(app, w)
                    .data_directory(data_dir.clone())
                    .build()?;
            }
            Ok(())
        })
        .run(context)
        .expect("не запустить окно");
}
