# build.ps1 — 交叉编译 p2p 实例的可执行程序。
#
# 默认生成 CentOS 可用的 coordinator（linux/amd64，静态二进制，无 glibc 依赖）。
# 用法：
#   ./build.ps1                          # coordinator, linux/amd64
#   ./build.ps1 -OS windows -Arch amd64  # coordinator, windows/amd64
#   ./build.ps1 -All                     # coordinator + peer，全部平台
param(
    [string]$OS = "linux",
    [string]$Arch = "amd64",
    [switch]$All
)

$ErrorActionPreference = "Stop"
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
Push-Location $here
try {
    $targets = @(
        @{ Name = "coordinator"; Dir = "./coordinator" },
        @{ Name = "peer";        Dir = "./peer" }
    )
    if (-not $All) {
        $targets = $targets | Where-Object { $_.Name -eq "coordinator" }
    }

    foreach ($t in $targets) {
        $out = "$($t.Name)-$OS-$Arch"
        $env:GOOS = $OS
        $env:GOARCH = $Arch
        $env:CGO_ENABLED = "0"
        go build -o $out $t.Dir
        if ($LASTEXITCODE -ne 0) {
            Write-Error "build $($t.Name) failed"
        }
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
        Write-Host "OK  $out"
    }
}
finally {
    Pop-Location
}
