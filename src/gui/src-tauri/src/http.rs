// Простейший HTTP-клиент к петлевому адресу — из wsldrive, дополненный
// методом и телом запроса.
//
// Запрос идёт по HTTP/1.0: на него сервер не отвечает кусками (chunked), и
// ответ читается просто до закрытия соединения.

use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::Duration;

/// Сколько ждать подключения. К порту, который никто не слушает, Windows
/// добирается две секунды, повторяя попытки, — окно замирало бы на них при
/// каждом обновлении (урок wsldrive, Р-042 там).
pub const CONNECT: Duration = Duration::from_millis(500);

/// Сколько ждать ответа после подключения. Изменяющий запрос может
/// останавливать ssh, а это до трёх секунд.
pub const IO: Duration = Duration::from_secs(8);

/// Ответ: код и тело.
pub struct Reply {
    pub code: u16,
    pub body: String,
}

pub fn request(port: u16, method: &str, path: &str, body: Option<&str>) -> Result<Reply, String> {
    let addr = format!("127.0.0.1:{port}");
    let sock = addr.parse().map_err(|e| format!("неверный адрес {addr}: {e}"))?;
    let mut stream = TcpStream::connect_timeout(&sock, CONNECT)
        .map_err(|e| format!("сервер не отвечает на {addr}: {e}"))?;
    stream.set_read_timeout(Some(IO)).ok();
    stream.set_write_timeout(Some(IO)).ok();

    let body = body.unwrap_or("");
    // X-Hotpipe — пропуск для изменяющих запросов: без него сервер их
    // отвергает, чтобы до него не дотянулась страница в браузере.
    let req = format!(
        "{method} {path} HTTP/1.0\r\nHost: 127.0.0.1\r\nX-Hotpipe: 1\r\n\
         Content-Type: application/json\r\nContent-Length: {}\r\n\r\n{body}",
        body.len()
    );
    stream
        .write_all(req.as_bytes())
        .map_err(|e| format!("запрос не отправлен: {e}"))?;

    let mut raw = Vec::new();
    stream
        .read_to_end(&mut raw)
        .map_err(|e| format!("ответ не прочитан: {e}"))?;
    parse(&raw)
}

fn parse(raw: &[u8]) -> Result<Reply, String> {
    let text = String::from_utf8_lossy(raw);
    let i = text.find("\r\n\r\n").ok_or("ответ без заголовков")?;
    let code = text
        .split_whitespace()
        .nth(1)
        .and_then(|c| c.parse().ok())
        .ok_or("ответ без кода")?;
    Ok(Reply { code, body: text[i + 4..].to_string() })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_code_and_body() {
        let r = parse(b"HTTP/1.0 404 Not Found\r\nContent-Length: 2\r\n\r\n{}").unwrap();
        assert_eq!(r.code, 404);
        assert_eq!(r.body, "{}");
    }
}
