# Управление настоящим окном hotpipe через автоматизацию UI Windows (по образцу
# wsldrive). Проверять надо в самом приложении: Edge без окна показывает
# страницу мимо CSP, который Tauri навязывает окну.
#
#   uia.ps1 dump                 флажки и кнопки окна
#   uia.ps1 toggle autostart     щёлкнуть флажок «Запускать при входе в Windows»
#   uia.ps1 click settings|save|cancel|add
param([string]$Action = "dump", [string]$What = "")

[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$CT = [System.Windows.Automation.ControlType]

# Русские имена — здесь, а не в командной строке: так они не зависят от того,
# как их донесёт вызывающая сторона.
$names = @{
    autostart = "Запускать при входе в Windows"; server = "Сервер hotpipe:"; settings = "Настройки"
    save = "Сохранить"; cancel = "Отмена"; add = "+ Добавить туннель"
}

$p = @(Get-Process hotpipe-gui -EA SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 })
if ($p.Count -ne 1) { "окон: $($p.Count), нужно ровно одно"; exit 1 }
$root = $A::FromHandle($p[0].MainWindowHandle)

function Find-All($ctype) {
    $c = New-Object System.Windows.Automation.PropertyCondition($A::ControlTypeProperty, $ctype)
    # Chromium строит дерево доступности лениво: первое обращение его будит.
    for ($i = 0; $i -lt 3; $i++) {
        $r = $root.FindAll([System.Windows.Automation.TreeScope]::Descendants, $c)
        if ($r.Count -gt 0) { return $r }
        Start-Sleep -Milliseconds 300
    }
    return $r
}

switch ($Action) {
    "dump" {
        foreach ($t in @($CT::CheckBox, $CT::Button)) {
            foreach ($e in (Find-All $t)) { "{0,-10} {1}" -f $t.ProgrammaticName.Split('.')[-1], $e.Current.Name }
        }
    }
    "toggle" {
        $want = $names[$What]
        $e = (Find-All $CT::CheckBox) | Where-Object { $_.Current.Name -like "*$want*" } | Select-Object -First 1
        if (-not $e) { "флажок «$want» не найден"; exit 1 }
        $tp = $e.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern)
        $tp.Toggle()
        "переключено: $want"
    }
    "click" {
        $want = $names[$What]
        $e = (Find-All $CT::Button) | Where-Object { $_.Current.Name -eq $want } | Select-Object -First 1
        if (-not $e) { "кнопка «$want» не найдена"; exit 1 }
        $e.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
        "нажато: $want"
    }
}
