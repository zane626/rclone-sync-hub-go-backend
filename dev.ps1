# 本地开发：使用 config.dev.yaml + Fresh 热重载（需已启动 MySQL：docker compose up mysql -d）
# 1. 确保 Go 安装的 fresh 在 PATH 中（go install 默认在 $GOPATH/bin 或 $HOME/go/bin）
$goBin = if ($env:GOPATH) { "$env:GOPATH\bin" } else { "$env:USERPROFILE\go\bin" }
if (Test-Path $goBin) { $env:Path = "$goBin;$env:Path" }

$freshExe = Get-Command fresh -ErrorAction SilentlyContinue
if (-not $freshExe) {
    Write-Host "未找到 fresh，请先安装：go install github.com/zzwx/fresh@latest" -ForegroundColor Yellow
    exit 1
}

# 2. 使用绝对路径设置 CONFIG_PATH，避免 Fresh 在 tmp 目录运行时找不到相对路径
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$configPath = Join-Path $scriptRoot "configs\config.dev.yaml"
$env:CONFIG_PATH = $configPath

# 3. 启动 fresh
& $freshExe.Source
