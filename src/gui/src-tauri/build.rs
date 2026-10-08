// Версия у всех программ проекта одна: по ней видно, все ли части обновлены.
// Сюда она приходит переменными окружения от build\build-gui.ps1 — окно
// собирается во временной копии вне хранилища и выяснить номер само не может.
// Без сценария сборки версия честно объявляет себя неизвестной.
fn main() {
    for (key, fallback) in [
        ("HOTPIPE_VERSION", "0.1.0-alpha"),
        ("HOTPIPE_COMMIT", "неизвестно"),
        ("HOTPIPE_BUILT", "неизвестно"),
        ("HOTPIPE_DIRTY", "yes"),
    ] {
        println!("cargo:rerun-if-env-changed={key}");
        let value = std::env::var(key).unwrap_or_else(|_| fallback.to_string());
        println!("cargo:rustc-env={key}={value}");
    }
    // Страница вшивается в программу при компиляции, а Tauri сам просит
    // пересборку лишь при правке tauri.conf.json (урок wsldrive).
    println!("cargo:rerun-if-changed=../ui");
    tauri_build::build()
}
