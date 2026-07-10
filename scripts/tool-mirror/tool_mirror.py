#!/usr/bin/env python3
"""JianVideo 工具镜像的锁定、校验、发现、打包和物料清单入口。"""

from __future__ import annotations

import argparse
import datetime
import hashlib
import html
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
DEFAULT_LOCK = ROOT / "lock.json"
MSYS_BASH = Path(r"C:\msys64\usr\bin\bash.exe")
MSYS_PREFIXES = {"UCRT64": "/ucrt64", "CLANGARM64": "/clangarm64"}
SHA256_LENGTH = 64
GIT_COMMIT_LENGTH = 40
ZIP_MIN_EPOCH = 315532800
PGP_KEY_PATTERN = re.compile(
    r"-----BEGIN PGP PUBLIC KEY BLOCK-----.*?-----END PGP PUBLIC KEY BLOCK-----",
    re.DOTALL,
)
VERIFICATION_METHODS = {
    "pgp",
    "github_release_asset",
    "github_attestation",
    "immutable_git_commit",
}


class MirrorError(RuntimeError):
    """表示必须阻断可信构建的错误。"""


def load_lock(path: Path) -> dict:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def valid_sha256(value: str) -> bool:
    return len(value) == SHA256_LENGTH and all(char in "0123456789abcdef" for char in value)


def valid_commit(value: str) -> bool:
    return len(value) == GIT_COMMIT_LENGTH and all(char in "0123456789abcdef" for char in value)


def runner_by_label(data: dict, label: str) -> dict:
    matches = [item for item in data.get("runners", []) if item.get("label") == label]
    if len(matches) != 1:
        raise MirrorError(f"runner 锁不存在或不唯一：{label}")
    return matches[0]


def validate_toolchain(runner: dict, required: bool) -> list[str]:
    errors: list[str] = []
    label = runner.get("label", "<未知 runner>")
    toolchain = runner.get("toolchain")
    if not isinstance(toolchain, dict):
        return [f"{label}: 缺少 toolchain 锁"]
    status = toolchain.get("status")
    if status == "blocked":
        if not toolchain.get("blocking_reason"):
            errors.append(f"{label}: 阻断工具链缺少 blocking_reason")
        if required:
            errors.append(f"{label}: {toolchain.get('blocking_reason', '工具链尚未锁定')}")
        return errors
    if status != "locked":
        return [f"{label}: toolchain.status 必须是 locked 或 blocked"]
    for field in ("image", "tools", "packages", "evidence"):
        if not toolchain.get(field):
            errors.append(f"{label}: 已锁工具链缺少 {field}")
    evidence = toolchain.get("evidence", {})
    if evidence.get("workflow_run") and not evidence["workflow_run"].startswith("https://github.com/"):
        errors.append(f"{label}: discovery workflow_run 必须是 GitHub HTTPS URL")
    if evidence.get("sha256") and not valid_sha256(evidence["sha256"]):
        errors.append(f"{label}: discovery 证据 SHA-256 格式无效")
    return errors


def validate_runner(runner: dict, require_toolchains: bool) -> list[str]:
    errors: list[str] = []
    label = runner.get("label", "<未知 runner>")
    for field in ("label", "id", "platform", "arch", "runner_os", "runner_arch"):
        if not runner.get(field):
            errors.append(f"{label}: 缺少 {field}")
    errors.extend(validate_toolchain(runner, require_toolchains))
    return errors


def validate_pgp(name: str, verification: dict) -> list[str]:
    errors: list[str] = []
    for field in ("signature_url", "key_url", "key_fingerprint", "key_format"):
        if not verification.get(field):
            errors.append(f"{name}: PGP verification 缺少 {field}")
    fingerprint = verification.get("key_fingerprint", "")
    if fingerprint and not re.fullmatch(r"[0-9A-F]{40,64}", fingerprint):
        errors.append(f"{name}: PGP 完整公钥指纹格式无效")
    if verification.get("key_format") not in (None, "armored_file", "html_armored_block"):
        errors.append(f"{name}: 不支持的 PGP key_format")
    return errors


def validate_github_asset(name: str, package: dict, verification: dict) -> list[str]:
    errors: list[str] = []
    for field in ("repository", "tag", "asset", "digest"):
        if not verification.get(field):
            errors.append(f"{name}: GitHub release verification 缺少 {field}")
    expected = f"sha256:{package.get('sha256', '')}"
    if verification.get("digest") and verification["digest"] != expected:
        errors.append(f"{name}: GitHub asset digest 与包 SHA-256 不一致")
    return errors


def validate_attestation(name: str, verification: dict) -> list[str]:
    errors: list[str] = []
    for field in (
        "bundle_url",
        "bundle_asset",
        "bundle_sha256",
        "source_digest",
        "source_ref",
        "signer_workflow",
    ):
        if not verification.get(field):
            errors.append(f"{name}: GitHub attestation verification 缺少 {field}")
    if verification.get("bundle_sha256") and not valid_sha256(verification["bundle_sha256"]):
        errors.append(f"{name}: attestation bundle SHA-256 格式无效")
    if verification.get("source_digest") and not valid_commit(verification["source_digest"]):
        errors.append(f"{name}: attestation source_digest 必须是完整 git commit")
    return errors


def validate_verification(package: dict) -> list[str]:
    name = package.get("name", "<未知>")
    verification = package.get("verification")
    if not isinstance(verification, dict):
        return [f"{name}: 缺少显式 verification 方法"]
    method = verification.get("method")
    if method not in VERIFICATION_METHODS:
        return [f"{name}: verification.method 不受支持或缺失"]
    if method == "pgp":
        return validate_pgp(name, verification)
    if method in ("github_release_asset", "github_attestation"):
        errors = validate_github_asset(name, package, verification)
        if method == "github_attestation":
            errors.extend(validate_attestation(name, verification))
        return errors
    commit = verification.get("commit", "")
    errors = []
    if not verification.get("repository"):
        errors.append(f"{name}: immutable_git_commit 缺少 repository")
    if not valid_commit(commit):
        errors.append(f"{name}: immutable_git_commit 必须锁定完整 commit")
    if commit and commit not in package.get("url", ""):
        errors.append(f"{name}: 源码 URL 未绑定已锁 commit")
    return errors


def validate_package(package: dict) -> list[str]:
    errors: list[str] = []
    name = package.get("name", "<未知>")
    for field in ("version", "license", "license_file", "archive_name", "url", "sha256"):
        if not package.get(field):
            errors.append(f"{name}: 缺少 {field}")
    if package.get("url") and not package["url"].startswith("https://"):
        errors.append(f"{name}: 源码 URL 必须使用 HTTPS")
    if package.get("sha256") and not valid_sha256(package["sha256"]):
        errors.append(f"{name}: SHA-256 格式无效")
    if package.get("blocking_reason"):
        errors.append(f"{name}: {package['blocking_reason']}")
    errors.extend(validate_verification(package))
    return errors


def validate_fixture_lock(data: dict, required: bool) -> list[str]:
    fixture = data.get("delegate_fixtures")
    if not isinstance(fixture, dict):
        return ["缺少 delegate_fixtures 锁"]
    status = fixture.get("status")
    if status == "blocked":
        if not fixture.get("blocking_reason"):
            return ["delegate fixture 阻断缺少 blocking_reason"]
        return [fixture["blocking_reason"]] if required else []
    if status != "locked":
        return ["delegate_fixtures.status 必须是 locked 或 blocked"]
    if not fixture.get("manifest"):
        return ["已锁 delegate fixture 缺少 manifest"]
    return []


def validate_lock(
    data: dict,
    include_optional: bool = False,
    require_toolchains: bool = True,
    require_fixtures: bool = True,
) -> list[str]:
    errors: list[str] = []
    if data.get("schema_version") != 2:
        errors.append("锁文件 schema_version 必须为 2")
    epoch = data.get("source_date_epoch")
    if not isinstance(epoch, int) or epoch < ZIP_MIN_EPOCH:
        errors.append("source_date_epoch 必须是 ZIP 可表示的固定 Unix 时间")
    runners = data.get("runners", [])
    labels = {row.get("label") for row in runners}
    ids = {row.get("id") for row in runners}
    if len(runners) != 6 or len(labels) != 6 or len(ids) != 6:
        errors.append("runner 矩阵必须恰好包含六个唯一标签和平台 ID")
    for runner in runners:
        errors.extend(validate_runner(runner, require_toolchains))
    errors.extend(validate_fixture_lock(data, require_fixtures))
    packages = data.get("packages", [])
    if not packages:
        errors.append("锁文件没有依赖项")
    for package in packages:
        if package.get("optional") and not include_optional:
            continue
        errors.extend(validate_package(package))
    return errors


def require_trusted_lock(data: dict, include_optional: bool = False) -> None:
    errors = validate_lock(data, include_optional)
    fixture = data.get("delegate_fixtures", {})
    if not errors and fixture.get("status") == "locked":
        manifest = ROOT / fixture["manifest"]
        if not manifest.is_file():
            errors.append(f"delegate fixture 清单不存在：{manifest}")
    if errors:
        raise MirrorError("可信构建预检失败：\n- " + "\n- ".join(errors))


def request_headers() -> dict[str, str]:
    headers = {"User-Agent": "JianVideo-tool-mirror/1"}
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def download(url: str, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    request = urllib.request.Request(url, headers=request_headers())
    with urllib.request.urlopen(request, timeout=120) as response, destination.open("wb") as output:
        shutil.copyfileobj(response, output)


def download_json(url: str) -> dict:
    request = urllib.request.Request(url, headers=request_headers())
    with urllib.request.urlopen(request, timeout=120) as response:
        return json.load(response)


def extract_armored_key(content: bytes, key_format: str) -> bytes:
    try:
        text = html.unescape(content.decode("utf-8"))
    except UnicodeDecodeError as error:
        raise MirrorError("官方公钥页面不是 UTF-8 文本") from error
    matches = PGP_KEY_PATTERN.findall(text)
    if len(matches) != 1:
        raise MirrorError("官方公钥来源必须且只能包含一个 PGP armor 块")
    block = matches[0].strip() + "\n"
    if key_format == "armored_file" and text.strip() != matches[0].strip():
        raise MirrorError("直接公钥文件包含 armor 之外的非空内容")
    if key_format not in ("armored_file", "html_armored_block"):
        raise MirrorError(f"不支持的公钥提取格式：{key_format}")
    return block.encode("ascii")


def imported_fingerprints(home: Path) -> set[str]:
    result = subprocess.run(
        ["gpg", "--homedir", str(home), "--batch", "--with-colons", "--fingerprint"],
        check=True,
        text=True,
        capture_output=True,
    )
    return {line.split(":")[9] for line in result.stdout.splitlines() if line.startswith("fpr:")}


def verify_pgp(package: dict, archive: Path, work: Path) -> None:
    verification = package["verification"]
    if not shutil.which("gpg"):
        raise MirrorError(f"{package['name']}: 需要 gpg 验证上游签名")
    signature = work / f"{archive.name}.asc"
    key_source = work / "key-source"
    key = work / "key.asc"
    download(verification["signature_url"], signature)
    download(verification["key_url"], key_source)
    key.write_bytes(extract_armored_key(key_source.read_bytes(), verification["key_format"]))
    home = work / "gnupg"
    home.mkdir(mode=0o700)
    subprocess.run(["gpg", "--homedir", str(home), "--batch", "--import", str(key)], check=True)
    fingerprint = verification["key_fingerprint"]
    if fingerprint not in imported_fingerprints(home):
        raise MirrorError(f"{package['name']}: 导入公钥指纹与锁文件不符")
    result = subprocess.run(
        ["gpg", "--homedir", str(home), "--batch", "--no-auto-key-retrieve", "--status-fd", "1", "--verify", str(signature), str(archive)],
        check=True,
        text=True,
        capture_output=True,
    )
    if f"VALIDSIG {fingerprint} " not in result.stdout:
        raise MirrorError(f"{package['name']}: 签名者指纹与锁文件不符")


def github_release(verification: dict) -> dict:
    repository = verification["repository"]
    tag = urllib.parse.quote(verification["tag"], safe="")
    return download_json(f"https://api.github.com/repos/{repository}/releases/tags/{tag}")


def release_asset(release: dict, name: str) -> dict:
    matches = [item for item in release.get("assets", []) if item.get("name") == name]
    if len(matches) != 1:
        raise MirrorError(f"GitHub release asset 不存在或不唯一：{name}")
    return matches[0]


def verify_release_asset_metadata(package: dict, release: dict) -> None:
    verification = package["verification"]
    asset = release_asset(release, verification["asset"])
    if asset.get("browser_download_url") != package["url"]:
        raise MirrorError(f"{package['name']}: GitHub asset URL 与锁文件不符")
    if asset.get("digest") != verification["digest"]:
        raise MirrorError(f"{package['name']}: GitHub asset digest 与锁文件不符")


def verify_github_attestation(package: dict, archive: Path, work: Path, release: dict) -> None:
    verification = package["verification"]
    if not shutil.which("gh"):
        raise MirrorError(f"{package['name']}: 需要 gh 验证 GitHub artifact attestation")
    bundle_asset = release_asset(release, verification["bundle_asset"])
    if bundle_asset.get("browser_download_url") != verification["bundle_url"]:
        raise MirrorError(f"{package['name']}: attestation bundle URL 与锁文件不符")
    expected_bundle_digest = f"sha256:{verification['bundle_sha256']}"
    if bundle_asset.get("digest") != expected_bundle_digest:
        raise MirrorError(f"{package['name']}: attestation bundle digest 与锁文件不符")
    bundle = work / verification["bundle_asset"]
    download(verification["bundle_url"], bundle)
    if sha256_file(bundle) != verification["bundle_sha256"]:
        raise MirrorError(f"{package['name']}: attestation bundle SHA-256 不匹配")
    command = [
        "gh", "attestation", "verify", str(archive), "--repo", verification["repository"],
        "--bundle", str(bundle), "--source-digest", verification["source_digest"],
        "--source-ref", verification["source_ref"], "--signer-workflow", verification["signer_workflow"],
        "--deny-self-hosted-runners",
    ]
    subprocess.run(command, check=True)


def verify_upstream(package: dict, archive: Path, work: Path) -> None:
    method = package["verification"]["method"]
    if method == "pgp":
        verify_pgp(package, archive, work)
        return
    if method == "immutable_git_commit":
        return
    release = github_release(package["verification"])
    verify_release_asset_metadata(package, release)
    if method == "github_attestation":
        verify_github_attestation(package, archive, work, release)


def fetch_sources(data: dict, cache: Path, include_optional: bool) -> None:
    require_trusted_lock(data, include_optional)
    cache.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="jianvideo-upstream-") as temp:
        work = Path(temp)
        for package in data["packages"]:
            if package.get("optional") and not include_optional:
                continue
            archive = cache / package["archive_name"]
            if not archive.exists():
                print(f"下载 {package['name']} {package['version']}")
                download(package["url"], archive)
            if sha256_file(archive) != package["sha256"]:
                archive.unlink(missing_ok=True)
                raise MirrorError(f"{package['name']}: SHA-256 不匹配")
            package_work = work / package["name"]
            package_work.mkdir()
            verify_upstream(package, archive, package_work)
            print(f"上游验证通过：{package['name']} {package['version']}")


def extract_archive(archive: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    if tarfile.is_tarfile(archive):
        with tarfile.open(archive) as source:
            for member in source.getmembers():
                target = (destination / member.name).resolve()
                if destination.resolve() not in target.parents and target != destination.resolve():
                    raise MirrorError(f"压缩包包含越界路径：{member.name}")
                if member.issym() or member.islnk():
                    raise MirrorError(f"压缩包包含链接：{member.name}")
            source.extractall(destination, filter="data")
        return
    if zipfile.is_zipfile(archive):
        with zipfile.ZipFile(archive) as source:
            for member in source.infolist():
                target = (destination / member.filename).resolve()
                if destination.resolve() not in target.parents and target != destination.resolve():
                    raise MirrorError(f"压缩包包含越界路径：{member.filename}")
            source.extractall(destination)
        return
    raise MirrorError(f"不支持的源码压缩格式：{archive.name}")


def assert_runner_identity(data: dict, label: str, actual_os: str, actual_arch: str) -> None:
    runner = runner_by_label(data, label)
    expected = (runner["runner_os"], runner["runner_arch"])
    actual = (actual_os, actual_arch)
    if actual != expected:
        raise MirrorError(f"{label}: runner 身份不匹配，期望 {expected[0]}/{expected[1]}，实际 {actual[0]}/{actual[1]}")


def first_output_line(result: subprocess.CompletedProcess[str]) -> str:
    lines = (result.stdout + "\n" + result.stderr).splitlines()
    return next((line.strip() for line in lines if line.strip()), "")


def capture_tool(name: str, command: list[str]) -> dict:
    path = shutil.which(command[0]) or ""
    if not path:
        return {"path": "", "version": "", "status": 127}
    result = subprocess.run(command, text=True, capture_output=True, check=False)
    return {"path": path, "version": first_output_line(result), "status": result.returncode}


def windows_msys_bash() -> Path:
    location = os.environ.get("MSYS2_LOCATION", "").strip()
    if location:
        return Path(location) / "usr" / "bin" / "bash.exe"
    return MSYS_BASH


def msys_environment(msystem: str) -> dict[str, str]:
    environment = os.environ.copy()
    environment.update({"MSYSTEM": msystem, "CHERE_INVOKING": "1", "MSYS2_PATH_TYPE": "minimal"})
    return environment


def msys_script(msystem: str, command: str) -> str:
    prefix = MSYS_PREFIXES.get(msystem)
    if not prefix:
        raise MirrorError(f"不支持的 MSYS2 环境：{msystem}")
    return f'export PATH="{prefix}/bin:/usr/local/bin:/usr/bin:/bin"; {command}'


def capture_msys_tool(bash: Path, msystem: str, name: str, command: str) -> dict:
    if not bash.is_file():
        return {"path": "", "version": "", "status": 127}
    script = msys_script(msystem, f"command -v {name}; {command}")
    result = subprocess.run(
        [str(bash), "--noprofile", "--norc", "-lc", script],
        env=msys_environment(msystem),
        text=True,
        capture_output=True,
        check=False,
    )
    lines = [line.strip() for line in (result.stdout + "\n" + result.stderr).splitlines() if line.strip()]
    path = lines[0] if lines and lines[0].startswith("/") else ""
    version = lines[1] if path and len(lines) > 1 else (lines[0] if lines else "")
    return {"path": path, "version": version, "status": result.returncode}


def unix_tools() -> dict[str, dict]:
    commands = {
        "cc": ["cc", "--version"], "cmake": ["cmake", "--version"],
        "make": ["make", "--version"], "pkg-config": ["pkg-config", "--version"],
        "autoconf": ["autoconf", "--version"], "automake": ["automake", "--version"],
        "libtoolize": ["libtoolize", "--version"], "tar": ["tar", "--version"],
        "python": ["python", "--version"], "gpg": ["gpg", "--version"],
        "gh": ["gh", "--version"],
    }
    if platform.system() == "Darwin":
        commands["libtoolize"] = ["glibtoolize", "--version"]
    return {name: capture_tool(name, command) for name, command in commands.items()}


def windows_tools(runner: dict) -> dict[str, dict]:
    msystem = runner["toolchain"]["shell"]["msystem"]
    commands = {
        "cc": ("cc", "cc --version"), "cmake": ("cmake", "cmake --version"),
        "make": ("make", "make --version"), "pkg-config": ("pkg-config", "pkg-config --version"),
        "autoconf": ("autoconf", "autoconf --version"), "automake": ("automake", "automake --version"),
        "libtoolize": ("libtoolize", "libtoolize --version"), "tar": ("tar", "tar --version"),
        "python": ("python", "python --version"),
    }
    bash = windows_msys_bash()
    tools = {name: capture_msys_tool(bash, msystem, executable, command) for name, (executable, command) in commands.items()}
    tools["host:gpg"] = capture_tool("gpg", ["gpg", "--version"])
    tools["host:gh"] = capture_tool("gh", ["gh", "--version"])
    tools["host:python"] = capture_tool("python", ["python", "--version"])
    return tools


def parse_packages(output: str, separator: str = " ") -> dict[str, str]:
    packages: dict[str, str] = {}
    for line in output.splitlines():
        if separator not in line:
            continue
        name, version = line.split(separator, 1)
        packages[name.strip()] = version.strip()
    return packages


def collect_packages(runner: dict) -> tuple[str, dict[str, str]]:
    if runner["platform"] == "windows":
        bash = windows_msys_bash()
        if not bash.is_file():
            return "unavailable", {}
        msystem = runner["toolchain"]["shell"]["msystem"]
        result = subprocess.run(
            [str(bash), "--noprofile", "--norc", "-lc", msys_script(msystem, "pacman -Q")],
            env=msys_environment(msystem),
            text=True,
            capture_output=True,
            check=False,
        )
        return "pacman", parse_packages(result.stdout)
    if platform.system() == "Darwin" and shutil.which("brew"):
        result = subprocess.run(["brew", "list", "--versions"], text=True, capture_output=True, check=False)
        return "brew", parse_packages(result.stdout)
    if shutil.which("dpkg-query"):
        result = subprocess.run(["dpkg-query", "-W", "-f=${Package}\t${Version}\n"], text=True, capture_output=True, check=False)
        return "dpkg", parse_packages(result.stdout, "\t")
    return "unknown", {}


def collect_discovery(data: dict, label: str) -> dict:
    runner = runner_by_label(data, label)
    assert_runner_identity(data, label, os.environ.get("RUNNER_OS", ""), os.environ.get("RUNNER_ARCH", ""))
    tools = windows_tools(runner) if runner["platform"] == "windows" else unix_tools()
    manager, packages = collect_packages(runner)
    return {
        "schema_version": 1,
        "runner": {key: runner[key] for key in ("label", "id", "platform", "arch", "runner_os", "runner_arch")},
        "github": {key: os.environ.get(key, "") for key in ("GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT")},
        "image": {key: os.environ.get(key, "") for key in ("ImageOS", "ImageVersion", "ImageLabel", "ImageVersionIncludedSoftware")},
        "system": {"platform": platform.platform(), "python": sys.version, "machine": platform.machine()},
        "environment": {key: os.environ.get(key, "") for key in ("RUNNER_OS", "RUNNER_ARCH", "MSYSTEM", "MSYS2_PATH_TYPE")},
        "tool_versions": tools,
        "package_manager": manager,
        "packages": packages,
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }


def write_discovery(data: dict, label: str, destination: Path) -> None:
    evidence = collect_discovery(data, label)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"discovery 证据已写入：{destination}")
    print(f"discovery 证据 SHA-256：{sha256_file(destination)}")


def verify_toolchain(data: dict, label: str) -> None:
    runner = runner_by_label(data, label)
    toolchain = runner["toolchain"]
    if toolchain.get("status") != "locked":
        raise MirrorError(f"{label}: {toolchain.get('blocking_reason', '工具链尚未锁定')}")
    current = collect_discovery(data, label)
    for key, expected in toolchain["image"].items():
        if current["image"].get(key) != expected:
            raise MirrorError(f"{label}: runner image {key} 与工具链锁不匹配")
    for name, expected in toolchain["tools"].items():
        actual = current["tool_versions"].get(name, {})
        if actual.get("status") != 0 or actual.get("path") != expected["path"] or actual.get("version") != expected["version"]:
            raise MirrorError(f"{label}: 工具 {name} 与锁定版本或路径不匹配")
    for name, expected in toolchain["packages"].items():
        if current["packages"].get(name) != expected:
            raise MirrorError(f"{label}: 构建包 {name} 与锁定版本不匹配")
    if runner["platform"] == "windows":
        prefix = toolchain["shell"]["prefix"] + "/bin/"
        if not current["tool_versions"]["cc"]["path"].startswith(prefix):
            raise MirrorError(f"{label}: 编译器不在锁定的 {toolchain['shell']['msystem']} 前缀中")
    print(f"工具链精确匹配：{label}")


def run_build(data: dict, lock_path: Path, runner: str, cache: Path, output: Path, include_optional: bool) -> None:
    require_trusted_lock(data, include_optional)
    verify_toolchain(data, runner)
    fetch_sources(data, cache, include_optional)
    if os.name == "nt":
        command = [
            "pwsh", "-NoProfile", "-File", str(ROOT / "build-windows.ps1"),
            "-Cache", str(cache), "-Output", str(output), "-Lock", str(lock_path), "-Runner", runner,
        ]
        if include_optional:
            command.append("-EnableHeicWrite")
    else:
        command = ["bash", str(ROOT / "build-unix.sh"), str(cache), str(output), "--enable-heic-write" if include_optional else "", str(lock_path), runner]
    subprocess.run(command, check=True)


def create_manifest(data: dict, payload: Path, destination: Path) -> None:
    files = []
    for path in sorted(item for item in payload.rglob("*") if item.is_file()):
        files.append({"path": path.relative_to(payload).as_posix(), "size": path.stat().st_size, "sha256": sha256_file(path)})
    document = {"schema_version": 1, "release": data["release"], "files": files}
    destination.write_text(json.dumps(document, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def create_sbom(data: dict, destination: Path, include_optional: bool) -> None:
    components = []
    for package in data["packages"]:
        if package.get("optional") and not include_optional:
            continue
        components.append({
            "type": "library", "name": package["name"], "version": package["version"],
            "licenses": [{"license": {"id": package["license"]}}],
            "hashes": [{"alg": "SHA-256", "content": package["sha256"]}],
            "externalReferences": [{"type": "distribution", "url": package["url"]}],
            "properties": [{"name": "jianvideo:upstream-verification", "value": package["verification"]["method"]}],
        })
    sbom = {"bomFormat": "CycloneDX", "specVersion": "1.6", "version": 1, "components": components}
    destination.write_text(json.dumps(sbom, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def collect_licenses(data: dict, source_root: Path, destination: Path, include_optional: bool) -> None:
    patterns = {"imagemagick": "ImageMagick-*", "libraw": "LibRaw-*", "libtiff": "libtiff-*"}
    destination.mkdir(parents=True, exist_ok=True)
    for package in data["packages"]:
        if package.get("optional") and not include_optional:
            continue
        directory = patterns.get(package["name"], f"{package['name']}*")
        matches = list(source_root.glob(f"{directory}/{package['license_file']}"))
        if len(matches) != 1:
            raise MirrorError(f"{package['name']}: 未唯一找到许可证文件 {package['license_file']}")
        shutil.copy2(matches[0], destination / f"{package['name']}-{package['version']}.txt")


def zip_timestamp(epoch: int) -> tuple[int, int, int, int, int, int]:
    value = time.gmtime(epoch)
    return value.tm_year, value.tm_mon, value.tm_mday, value.tm_hour, value.tm_min, value.tm_sec


def package_payload(data: dict, payload: Path, destination: Path) -> None:
    if destination.suffix.lower() != ".zip":
        raise MirrorError("六个平台发布产物必须统一为 ZIP")
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.unlink(missing_ok=True)
    timestamp = zip_timestamp(data["source_date_epoch"])
    with zipfile.ZipFile(destination, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as output:
        for path in sorted(item for item in payload.rglob("*") if item.is_file()):
            relative = path.relative_to(payload).as_posix()
            mode = 0o755 if relative.startswith("bin/") else 0o644
            info = zipfile.ZipInfo(relative, timestamp)
            info.create_system = 3
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = mode << 16
            info.extra = b""
            info.comment = b""
            output.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
    print(f"已生成确定性 ZIP：{destination}")


def verify_binary_versions(data: dict, binary_root: Path) -> None:
    versions = {item["name"]: item["version"] for item in data["packages"]}
    extension = ".exe" if os.name == "nt" else ""
    checks = (("ffmpeg", versions["ffmpeg"]), ("ffprobe", versions["ffmpeg"]), ("magick", versions["imagemagick"]))
    for name, version in checks:
        binary = binary_root / f"{name}{extension}"
        result = subprocess.run([str(binary), "-version"], check=True, text=True, capture_output=True)
        if version not in result.stdout:
            raise MirrorError(f"{name}: 版本输出与锁文件不匹配")
    print("ffmpeg、ffprobe、magick 版本验证通过")


def add_commands(parser: argparse.ArgumentParser) -> None:
    sub = parser.add_subparsers(dest="command", required=True)
    preflight = sub.add_parser("preflight")
    preflight.add_argument("--runner")
    identity = sub.add_parser("assert-runner")
    identity.add_argument("--runner", required=True)
    identity.add_argument("--runner-os", required=True)
    identity.add_argument("--runner-arch", required=True)
    discovery = sub.add_parser("discover")
    discovery.add_argument("output", type=Path)
    discovery.add_argument("--runner", required=True)
    verify = sub.add_parser("verify-toolchain")
    verify.add_argument("--runner", required=True)
    fetch = sub.add_parser("fetch")
    fetch.add_argument("cache", type=Path)
    build = sub.add_parser("build")
    build.add_argument("cache", type=Path)
    build.add_argument("output", type=Path)
    build.add_argument("--runner", required=True)
    manifest = sub.add_parser("manifest")
    manifest.add_argument("payload", type=Path)
    manifest.add_argument("output", type=Path)
    sbom = sub.add_parser("sbom")
    sbom.add_argument("output", type=Path)
    licenses = sub.add_parser("licenses")
    licenses.add_argument("sources", type=Path)
    licenses.add_argument("output", type=Path)
    package = sub.add_parser("package")
    package.add_argument("payload", type=Path)
    package.add_argument("output", type=Path)
    binaries = sub.add_parser("verify-binaries")
    binaries.add_argument("bin", type=Path)


def execute(args: argparse.Namespace, data: dict) -> None:
    if args.command == "preflight":
        require_trusted_lock(data, args.enable_heic_write)
        if not args.runner:
            raise MirrorError("正式预检必须指定并核对当前 runner")
        verify_toolchain(data, args.runner)
        print("可信构建预检通过")
    elif args.command == "assert-runner":
        assert_runner_identity(data, args.runner, args.runner_os, args.runner_arch)
    elif args.command == "discover":
        write_discovery(data, args.runner, args.output)
    elif args.command == "verify-toolchain":
        verify_toolchain(data, args.runner)
    elif args.command == "fetch":
        fetch_sources(data, args.cache, args.enable_heic_write)
    elif args.command == "build":
        run_build(data, args.lock, args.runner, args.cache, args.output, args.enable_heic_write)
    elif args.command == "manifest":
        create_manifest(data, args.payload, args.output)
    elif args.command == "sbom":
        create_sbom(data, args.output, args.enable_heic_write)
    elif args.command == "licenses":
        collect_licenses(data, args.sources, args.output, args.enable_heic_write)
    elif args.command == "package":
        package_payload(data, args.payload, args.output)
    elif args.command == "verify-binaries":
        verify_binary_versions(data, args.bin)


def main() -> int:
    parser = argparse.ArgumentParser(description="JianVideo 可信工具镜像脚本")
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK)
    parser.add_argument("--enable-heic-write", action="store_true")
    add_commands(parser)
    args = parser.parse_args()
    try:
        execute(args, load_lock(args.lock))
    except (MirrorError, OSError, ValueError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print(f"错误：{error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
