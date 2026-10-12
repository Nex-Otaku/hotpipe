# Установка

Для Windows + WSL. Установка на macOS — в форке
[alidiab98/hotpipe](https://github.com/alidiab98/hotpipe).

Из WSL — сервер:

```bash
build/build.sh       # собрать и прогнать проверки
build/install.sh     # положить в ~/.local/bin (и перезапустить работающий)
```

Из PowerShell на Windows — окно (нужны Rust и cargo):

```powershell
build\build-gui.ps1
build\deploy.ps1 -Start      # в D:\Dev\projects\hotpipe и запустить
```

В окне — галочка **«Запускать при входе в Windows»**. Она записывает сторожа в
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`; права администратора не
нужны.

**Вход на SSH-сервер должен идти по ключу без парольной фразы.** Сервер
работает без человека и не может спросить пароль (`BatchMode=yes`). Если туннель
пишет «сервер не принял ключ» — `ssh-copy-id -p <порт> <пользователь>@<сервер>`.
