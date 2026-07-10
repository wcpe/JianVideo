# 在 GitHub 官方 Windows runner 上使用精确锁定的 UCRT64/CLANGARM64 环境执行静态构建。
param(
    [Parameter(Mandatory = $true)][string]$Cache,
    [Parameter(Mandatory = $true)][string]$Output,
    [Parameter(Mandatory = $true)][string]$Lock,
    [Parameter(Mandatory = $true)][string]$Runner,
    [switch]$EnableHeicWrite
)

$ErrorActionPreference = "Stop"
$msysRoot = if ($env:MSYS2_LOCATION) { $env:MSYS2_LOCATION } else { "C:\msys64" }
$bash = Join-Path $msysRoot "usr\bin\bash.exe"
if (-not (Test-Path $bash)) {
    throw "错误：未找到由固定版本安装步骤提供的 MSYS2：$bash"
}

$lockData = Get-Content -Raw -Encoding UTF8 $Lock | ConvertFrom-Json
$runnerLocks = @($lockData.runners | Where-Object { $_.label -eq $Runner })
if ($runnerLocks.Count -ne 1) {
    throw "错误：runner 锁不存在或不唯一：$Runner"
}
$runnerLock = $runnerLocks[0]
$msystem = $runnerLock.toolchain.shell.msystem
$prefix = $runnerLock.toolchain.shell.prefix
$requiredSystem = if ($runnerLock.runner_arch -eq "ARM64") { "CLANGARM64" } else { "UCRT64" }
if ($msystem -ne $requiredSystem) {
    throw "错误：$Runner 必须显式使用 $requiredSystem，锁文件实际为 $msystem。"
}

$actualArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToUpperInvariant()
if ($actualArch -ne $runnerLock.runner_arch) {
    throw "错误：Windows 架构不匹配，期望 $($runnerLock.runner_arch)，实际 $actualArch。"
}

$toolMirror = Join-Path $PSScriptRoot "tool_mirror.py"
& python $toolMirror --lock $Lock verify-toolchain --runner $Runner
if ($LASTEXITCODE -ne 0) {
    throw "错误：Windows 工具链与锁文件不匹配。"
}

function Convert-ToMsysPath([string]$Path) {
    $fullPath = [IO.Path]::GetFullPath($Path)
    $drive = $fullPath.Substring(0, 1).ToLowerInvariant()
    $suffix = $fullPath.Substring(2).Replace("\", "/")
    return "/$drive$suffix"
}

$script = Convert-ToMsysPath (Join-Path $PSScriptRoot "build-unix.sh")
$cachePath = Convert-ToMsysPath (Resolve-Path $Cache).Path
$outputPath = Convert-ToMsysPath $Output
$lockPath = Convert-ToMsysPath (Resolve-Path $Lock).Path
$runnerTempPath = if ($env:RUNNER_TEMP) { Convert-ToMsysPath $env:RUNNER_TEMP } else { "" }
$heicArgument = if ($EnableHeicWrite) { "--enable-heic-write" } else { "" }
$env:MSYSTEM = $msystem
$env:CHERE_INVOKING = "1"
$env:MSYS2_PATH_TYPE = "minimal"

$guard = @'
test "$MSYSTEM" = "$1" || { echo "错误：MSYSTEM 与锁文件不匹配" >&2; exit 1; }
export PATH="$2/bin:/usr/local/bin:/usr/bin:/bin"
[ -z "${9:-}" ] || export RUNNER_TEMP="$9"
compiler="$(command -v cc)"
case "$compiler" in
  "$2"/bin/*) ;;
  *) echo "错误：禁止使用 /usr 或其他未锁定编译器：$compiler" >&2; exit 1 ;;
esac
TOOLCHAIN_VERIFIED=1 exec "$3" "$4" "$5" "$6" "$7" "$8"
'@
& $bash --noprofile --norc -lc $guard "tool-mirror" $msystem $prefix $script $cachePath $outputPath $heicArgument $lockPath $Runner $runnerTempPath
if ($LASTEXITCODE -ne 0) {
    throw "错误：Windows 静态构建失败，退出码 $LASTEXITCODE。"
}
Write-Host "Windows 构建完成：$Output"
