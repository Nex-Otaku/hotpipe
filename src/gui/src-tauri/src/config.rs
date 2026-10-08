// Настройки окна: gui-config.json рядом с hotpipe-gui.exe.
//
// Рядом, а не в профиле пользователя: программу переносят папкой, и настройки
// должны переезжать с ней (так же в wsldrive). Отсутствие файла — не ошибка, а
// первое знакомство.
//
// Туннелей здесь нет: они живут у сервера, в WSL. Окно своего учёта не ведёт —
// два списка туннелей рано или поздно разошлись бы.

use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    /// Дистрибутив WSL. Пусто — тот, что выбран по умолчанию.
    #[serde(default)]
    pub distro: String,

    /// Порт API сервера.
    #[serde(default = "default_port")]
    pub api_port: u16,

    /// Путь к hotpipe-server внутри WSL. Пусто — ~/.local/bin/hotpipe-server,
    /// куда его кладёт build/install.sh.
    #[serde(default)]
    pub server_binary: String,

    /// Должен ли сервер работать. Это желание человека, и сторож следует ему:
    /// выключенный кнопкой сервер не поднимается сам ни сторожем, ни при
    /// открытии окна.
    #[serde(default = "default_true")]
    pub server_enabled: bool,
}

fn default_port() -> u16 {
    46310
}
fn default_true() -> bool {
    true
}

impl Default for Config {
    fn default() -> Self {
        Config {
            distro: String::new(),
            api_port: default_port(),
            server_binary: String::new(),
            server_enabled: true,
        }
    }
}

pub fn app_dir() -> PathBuf {
    std::env::current_exe()
        .ok()
        .and_then(|p| p.parent().map(|d| d.to_path_buf()))
        .unwrap_or_else(|| PathBuf::from("."))
}

pub fn config_path() -> PathBuf {
    app_dir().join("gui-config.json")
}

/// Испорченный файл не мешает работе: о нём сообщаем, но берём умолчания —
/// иначе окно нельзя было бы открыть, чтобы его же и исправить.
pub fn load() -> (Config, Option<String>) {
    let path = config_path();
    let raw = match std::fs::read_to_string(&path) {
        Ok(s) => s,
        Err(_) => return (Config::default(), None),
    };
    // Метку кодировки ставят Блокнот и PowerShell 5.1, а разборщик JSON на ней
    // спотыкается.
    let raw = raw.strip_prefix('\u{feff}').unwrap_or(&raw);
    match serde_json::from_str::<Config>(raw) {
        Ok(cfg) => (cfg, None),
        Err(e) => (
            Config::default(),
            Some(format!("{} разобрать не удалось ({e}); взяты значения по умолчанию", path.display())),
        ),
    }
}

pub fn save(cfg: &Config) -> Result<(), String> {
    let path = config_path();
    let body = serde_json::to_string_pretty(cfg).map_err(|e| e.to_string())?;
    std::fs::write(&path, body).map_err(|e| format!("не записать {}: {e}", path.display()))
}
