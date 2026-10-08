// Журнал окна и сторожа: hotpipe-gui.log рядом с программой.
//
// Пишет в основном сторож — у него нет ни окна, ни консоли, и без файла не
// узнать, почему сервер не поднимается. Файл открывается на каждую запись:
// окно и сторож — разные процессы и пишут в него оба.

use crate::config;
use std::fs::OpenOptions;
use std::io::Write;
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

const ROTATE_BYTES: u64 = 1 << 20;

pub fn path() -> PathBuf {
    config::app_dir().join("hotpipe-gui.log")
}

pub fn line(who: &str, text: &str) {
    let p = path();
    if let Ok(meta) = std::fs::metadata(&p) {
        if meta.len() > ROTATE_BYTES {
            let _ = std::fs::rename(&p, p.with_extension("log.1"));
        }
    }
    // Ошибка записи журнала не должна мешать делу.
    if let Ok(mut f) = OpenOptions::new().create(true).append(true).open(&p) {
        let _ = writeln!(f, "{} [{who}] {text}", stamp());
    }
}

/// Время в UTC с явной пометкой: местное без лишней зависимости не получить,
/// а время без пояснения, какое оно, хуже никакого.
fn stamp() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0);
    let (days, rem) = (secs.div_euclid(86_400), secs.rem_euclid(86_400));
    let (y, m, d) = civil_from_days(days);
    format!("{y:04}-{m:02}-{d:02} {:02}:{:02}:{:02}Z", rem / 3600, rem % 3600 / 60, rem % 60)
}

/// Дни от 1970-01-01 в календарную дату (алгоритм Говарда Хиннанта).
fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = z.div_euclid(146_097);
    let doe = z.rem_euclid(146_097);
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

#[cfg(test)]
mod tests {
    #[test]
    fn civil() {
        assert_eq!(super::civil_from_days(0), (1970, 1, 1));
        assert_eq!(super::civil_from_days(20_733), (2026, 10, 7));
    }
}
