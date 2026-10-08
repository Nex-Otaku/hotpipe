# Номер сборки — единый для всех программ проекта: по нему окно видит, что
# сервер отстал (или наоборот). Та же формула, что в build.sh:
# 0.1.<число снимков>-alpha.

function Get-HotpipeVersion {
    param([Parameter(Mandatory = $true)][string]$Root)

    # Вывод git в поток ошибок при ErrorActionPreference = Stop PowerShell 5.1
    # превращает в исключение — и сборка вне хранилища обрывалась на «not a git
    # repository», хотя этот случай здесь же и обработан.
    $ErrorActionPreference = "Continue"
    Push-Location $Root
    try {
        $null = git rev-parse HEAD 2>$null
        if ($LASTEXITCODE -eq 0) {
            $buildNum = (git rev-list --count HEAD).Trim()
            $commit = (git rev-parse --short HEAD).Trim()
            # Права файлов не учитываются: через \\wsl.localhost Windows-git не
            # видит бит исполнимости, и чистое дерево выглядело бы изменённым.
            git -c core.fileMode=false diff --quiet HEAD 2>$null
            $dirty = if ($LASTEXITCODE -eq 0) { "no" } else { "yes" }
        } else {
            $buildNum = "0"
            $commit = "вне-хранилища"
            $dirty = "yes"
        }
    } finally {
        Pop-Location
    }

    @{
        Number = "0.1.$buildNum-alpha"
        Commit = $commit
        Built  = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        Dirty  = $dirty
    }
}
