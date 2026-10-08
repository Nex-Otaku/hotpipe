# Снимок окна HotPipe (из wsldrive) — только самого окна, не экрана.
#
# Именно окна: снимок области экрана захватил бы и то, что лежит поверх него, —
# чужие окна с чужим содержимым. PrintWindow с флагом PW_RENDERFULLCONTENT
# отрисовывает содержимое окна на основе WebView2 само по себе.
#
#   shot.ps1 D:\путь\снимок.png
#   shot.ps1 D:\путь\снимок.png -ProcessId 9456    # когда окон несколько
#
# Свёрнутое окно снимать бесполезно: содержимое WebView2 в нём не отрисовано, и
# снимок выйдет пустым. Об этом программа и скажет, вместо того чтобы отдать
# пустую картинку за настоящую.
param([string]$Path, [int]$ProcessId = 0)

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;
public class W2 {
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L,T,R,B; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint flags);
  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr h);
}
"@
# Окон бывает больше одного: рядом с боевой раскладкой держат испытательную
# копию, и выглядят они одинаково. Снять не то окно значит принять чужое
# состояние за своё — поэтому при нескольких окнах программа требует назвать
# нужное, а не берёт первое попавшееся.
$all = @(Get-Process hotpipe-gui -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 })
if ($all.Count -eq 0) { Write-Output "окна нет"; exit 1 }

if ($ProcessId -gt 0) {
    $p = $all | Where-Object { $_.Id -eq $ProcessId } | Select-Object -First 1
    if (-not $p) { Write-Output "окна с номером $ProcessId нет"; exit 1 }
} elseif ($all.Count -gt 1) {
    Write-Output "окон больше одного — укажите, какое снимать: -ProcessId <номер>"
    $all | ForEach-Object { Write-Output ("  " + $_.Id + "  " + $_.Path) }
    exit 2
} else {
    $p = $all[0]
}

if ([W2]::IsIconic($p.MainWindowHandle)) {
    Write-Output "окно свёрнуто: снимок вышел бы пустым. Разверните его и повторите."
    exit 3
}
$r = New-Object W2+RECT
[W2]::GetWindowRect($p.MainWindowHandle, [ref]$r) | Out-Null
$w = $r.R - $r.L; $h = $r.B - $r.T
$bmp = New-Object System.Drawing.Bitmap $w, $h
$g = [System.Drawing.Graphics]::FromImage($bmp)
$dc = $g.GetHdc()
# PW_RENDERFULLCONTENT = 2: нужен для окон на основе WebView2
[W2]::PrintWindow($p.MainWindowHandle, $dc, 2) | Out-Null
$g.ReleaseHdc($dc)
$bmp.Save($Path, [System.Drawing.Imaging.ImageFormat]::Png)
Write-Output "captured ${w}x${h}"
