// Автозапуск при входе в macOS: задание launchd в ~/Library/LaunchAgents.
//
// Запускается не окно, а сервер: после входа нужны работающие туннели, а не
// окно на экране. Описание то же, что окно загружает само (server_macos.rs), —
// с RunAtLoad и KeepAlive; права администратора не нужны (Р-010).

use crate::config;
use crate::server;
use std::path::PathBuf;

#[derive(serde::Serialize)]
pub struct State {
    pub enabled: bool,
    /// Сервер, к которому ведёт записанное задание, — чтобы заметить, что оно
    /// ведёт к другой копии программы (переложили HotPipe.app).
    pub command: String,
    pub matches: bool,
}

fn path() -> PathBuf {
    config::home().join("Library/LaunchAgents").join(format!("{}.plist", server::LABEL))
}

fn wanted() -> String {
    server::plist(&config::load().0)
}

/// Первая строка в ProgramArguments — путь к серверу.
fn program(body: &str) -> String {
    body.split_once("<key>ProgramArguments</key>")
        .and_then(|(_, rest)| rest.split_once("<string>"))
        .and_then(|(_, rest)| rest.split_once("</string>"))
        .map(|(p, _)| p.replace("&lt;", "<").replace("&gt;", ">").replace("&amp;", "&"))
        .unwrap_or_default()
}

pub fn get() -> State {
    match std::fs::read_to_string(path()) {
        Ok(body) => State { enabled: true, command: program(&body), matches: body == wanted() },
        Err(_) => State { enabled: false, command: String::new(), matches: false },
    }
}

fn write(body: &str) -> Result<(), String> {
    let p = path();
    if let Some(dir) = p.parent() {
        std::fs::create_dir_all(dir).map_err(|e| format!("не создать {}: {e}", dir.display()))?;
    }
    std::fs::write(&p, body).map_err(|e| format!("не записать {}: {e}", p.display()))
}

pub fn set(on: bool) -> Result<(), String> {
    if on {
        return write(&wanted());
    }
    match std::fs::remove_file(path()) {
        Ok(()) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(format!("не удалить {}: {e}", path().display())),
    }
}

/// Включённый автозапуск переписывается вместе с заданием: оба должны вести к
/// одному и тому же серверу.
pub fn refresh(body: &str) -> Result<(), String> {
    if path().exists() {
        write(body)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn program_is_first_argument() {
        let body = server::plist(&config::Config::default());
        assert!(program(&body).ends_with("hotpipe-server"), "{}", program(&body));
    }
}
