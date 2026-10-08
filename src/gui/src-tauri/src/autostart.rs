// Автозапуск при входе в Windows: строка в HKCU\...\Run.
//
// Запускается не окно, а сторож (--keeper): после входа нужны работающие
// туннели, а не окно на экране. Раздел текущего пользователя, а не машины, —
// права администратора не нужны. Пишем через reg.exe: библиотека реестра ради
// одной строки — лишние пакеты при сборке.

use crate::server::CREATE_NO_WINDOW;
use std::os::windows::process::CommandExt;
use std::process::Command;

const KEY: &str = r"HKCU\Software\Microsoft\Windows\CurrentVersion\Run";
const VALUE: &str = "hotpipe";

#[derive(serde::Serialize)]
pub struct State {
    pub enabled: bool,
    /// Записанная команда — чтобы заметить, что она ведёт к другой копии
    /// программы (переложили папку).
    pub command: String,
    pub matches: bool,
}

fn wanted() -> Result<String, String> {
    let exe = std::env::current_exe().map_err(|e| format!("не найти себя: {e}"))?;
    Ok(format!("\"{}\" --keeper", exe.display()))
}

pub fn get() -> State {
    let out = Command::new("reg.exe")
        .args(["query", KEY, "/v", VALUE])
        .creation_flags(CREATE_NO_WINDOW)
        .output();
    let command = match out {
        Ok(o) if o.status.success() => String::from_utf8_lossy(&o.stdout)
            .lines()
            .find_map(|l| l.split_once("REG_SZ").map(|(_, v)| v.trim().to_string()))
            .unwrap_or_default(),
        _ => String::new(),
    };
    let matches = wanted().map(|w| w.eq_ignore_ascii_case(&command)).unwrap_or(false);
    State { enabled: !command.is_empty(), command, matches }
}

pub fn set(on: bool) -> Result<(), String> {
    let out = if on {
        Command::new("reg.exe")
            .args(["add", KEY, "/v", VALUE, "/t", "REG_SZ", "/d", &wanted()?, "/f"])
            .creation_flags(CREATE_NO_WINDOW)
            .output()
    } else {
        Command::new("reg.exe")
            .args(["delete", KEY, "/v", VALUE, "/f"])
            .creation_flags(CREATE_NO_WINDOW)
            .output()
    }
    .map_err(|e| format!("не запустить reg.exe: {e}"))?;
    if out.status.success() || (!on && !get().enabled) {
        return Ok(());
    }
    Err(format!("reg.exe: {}", String::from_utf8_lossy(&out.stderr).trim()))
}
