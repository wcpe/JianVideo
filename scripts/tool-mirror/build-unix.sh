#!/usr/bin/env bash
# 从已验证源码和精确锁定工具链构建最小静态 ffmpeg/ffprobe/magick 工具集。
set -euo pipefail

cache="${1:?用法：build-unix.sh <源码缓存> <输出目录> <HEIC 写入开关> <锁文件> <runner>}"
output="${2:?用法：build-unix.sh <源码缓存> <输出目录> <HEIC 写入开关> <锁文件> <runner>}"
heic_write="${3:-}"
lock="${4:?缺少锁文件路径}"
runner="${5:?缺少 runner 标签}"
script_root="$(cd "$(dirname "$0")" && pwd)"
work="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/jianvideo-tool-build"
sources="$work/sources"
prefix="$work/prefix"

if [ "${TOOLCHAIN_VERIFIED:-}" != "1" ]; then
  python "$script_root/tool_mirror.py" --lock "$lock" verify-toolchain --runner "$runner"
fi

required=(cc c++ cmake make pkg-config tar autoreconf automake python)
if [ "${RUNNER_OS:-}" = "macOS" ]; then
  required+=(glibtoolize)
else
  required+=(libtoolize)
fi
for command_name in "${required[@]}"; do
  command -v "$command_name" >/dev/null || { echo "错误：缺少锁定构建工具 $command_name" >&2; exit 1; }
done

rm -rf "$work" "$output"
mkdir -p "$sources" "$prefix" "$output/bin"

# 只接受上游验证阶段写入缓存的归档，禁止构建阶段改走网络。
for archive in "$cache"/*; do
  [ -f "$archive" ] || { echo "错误：源码缓存为空" >&2; exit 1; }
  tar -xf "$archive" -C "$sources"
done

jobs="${NUMBER_OF_PROCESSORS:-$(getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')}"
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig:$prefix/share/pkgconfig"
export CFLAGS="-O2 -fPIC"
export CXXFLAGS="$CFLAGS"
export LDFLAGS="-L$prefix/lib"
export CPPFLAGS="-I$prefix/include"

source_dir() {
  local pattern="$1" found
  found=("$sources"/$pattern)
  [ "${#found[@]}" -eq 1 ] && [ -d "${found[0]}" ] || { echo "错误：源码目录不唯一：$pattern" >&2; exit 1; }
  printf '%s' "${found[0]}"
}

cmake_build() {
  local source="$1" name="$2"; shift 2
  cmake -S "$source" -B "$work/build-$name" -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$prefix" -DBUILD_SHARED_LIBS=OFF "$@"
  cmake --build "$work/build-$name" --parallel "$jobs"
  cmake --install "$work/build-$name"
}

configure_build() {
  local source="$1" name="$2"; shift 2
  (cd "$source" && ./configure --prefix="$prefix" --enable-static --disable-shared "$@" && make -j"$jobs" && make install)
}

zlib="$(source_dir 'zlib-*')"
(cd "$zlib" && ./configure --prefix="$prefix" --static && make -j"$jobs" && make install)
cmake_build "$(source_dir 'libpng-*')" libpng -DPNG_SHARED=OFF -DPNG_TESTS=OFF
cmake_build "$(source_dir 'libjpeg-turbo-*')" jpeg -DENABLE_SHARED=OFF -DWITH_TURBOJPEG=OFF
cmake_build "$(source_dir 'libwebp-*')" webp -DWEBP_BUILD_ANIM_UTILS=OFF -DWEBP_BUILD_CWEBP=OFF -DWEBP_BUILD_DWEBP=OFF -DWEBP_BUILD_EXTRAS=OFF
cmake_build "$(source_dir 'libtiff-*')" tiff -Dtiff-tools=OFF -Dtiff-tests=OFF -Dtiff-contrib=OFF -Dzstd=OFF -Dlzma=OFF -Dwebp=OFF -Djbig=OFF -Dlibdeflate=OFF
cmake_build "$(source_dir 'libde265-*')" libde265 -DENABLE_SDL=OFF -DENABLE_DEC265=OFF
if [ "$heic_write" = "--enable-heic-write" ]; then
  cmake_build "$(source_dir 'x265-*')/source" x265 -DENABLE_SHARED=OFF -DENABLE_CLI=OFF
fi
cmake_build "$(source_dir 'libheif-*')" libheif -DBUILD_SHARED_LIBS=OFF -DWITH_LIBDE265=ON -DWITH_X265=$([ "$heic_write" = "--enable-heic-write" ] && printf ON || printf OFF) -DWITH_EXAMPLES=OFF -DWITH_GDK_PIXBUF=OFF -DBUILD_TESTING=OFF -DCMAKE_DISABLE_FIND_PACKAGE_TIFF=TRUE -DCMAKE_DISABLE_FIND_PACKAGE_JPEG=TRUE -DCMAKE_DISABLE_FIND_PACKAGE_PNG=TRUE

libraw="$(source_dir 'LibRaw-*')"
libraw_args=(--prefix="$prefix" --enable-static --disable-shared --disable-examples)
if [ "${RUNNER_OS:-}" = "Windows" ]; then
  python - "$libraw/configure.ac" <<'PY'
from pathlib import Path
import re
import sys

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
legacy = "\n".join((
    "AC_PROG_LIBTOOL", "AC_ENABLE_SHARED", "AC_ENABLE_STATIC",
    "AC_LIBTOOL_WIN32_DLL", "AC_LIBTOOL_SETUP", "AC_SUBST(LIBTOOL_DEPS)",
))
replacement = "LT_INIT([win32-dll])"
if text.count(legacy) != 1:
    raise SystemExit("错误：LibRaw 旧版 Libtool 配置块不存在或不唯一")
text = text.replace(legacy, replacement, 1)
warnings = (
    "AC_MSG_WARN([no jpeg headers found])",
    "AC_MSG_WARN([libjpeg not found])",
    "AC_MSG_WARN([no jasper headers found])",
    "AC_MSG_WARN([libjasper not found])",
    "AC_MSG_WARN([zlib support cannot be enabled])",
)
for warning in warnings:
    if text.count(warning) != 1:
        raise SystemExit(f"错误：LibRaw 警告动作不存在或不唯一：{warning}")
    text = text.replace(warning, f"[{warning}]", 1)
text, count = re.subn(r"if test x\$openmp = xtrue ; then.*?\nfi", "openmp=false", text, count=1, flags=re.DOTALL)
if count != 1:
    raise SystemExit("错误：LibRaw OpenMP 配置块不存在或不唯一")
path.write_text(text, encoding="utf-8")
PY
  libraw_args+=(--disable-openmp)
fi
(cd "$libraw" && autoreconf -fi -I m4 && ./configure "${libraw_args[@]}" && make -j"$jobs" && make install)

x264="$(source_dir 'x264-*')"
x264_args=(--prefix="$prefix" --enable-static --disable-cli --disable-opencl)
case "$(uname -m)" in
  x86_64|amd64) command -v nasm >/dev/null || x264_args+=(--disable-asm) ;;
esac
(cd "$x264" && ./configure "${x264_args[@]}" && make -j"$jobs" && make install)

ffmpeg="$(source_dir 'ffmpeg-*')"
ffmpeg_args=(--prefix="$prefix" --pkg-config-flags=--static --extra-cflags="-I$prefix/include" --extra-ldflags="-L$prefix/lib" --enable-gpl --enable-libx264 --disable-doc --disable-debug --disable-ffplay --enable-static --disable-shared)
case "$(uname -m)" in
  x86_64|amd64) command -v nasm >/dev/null || ffmpeg_args+=(--disable-x86asm) ;;
esac
(cd "$ffmpeg" && ./configure "${ffmpeg_args[@]}" && make -j"$jobs" && make install)

magick="$(source_dir 'ImageMagick-*')"
delegate_libs="$(pkg-config --static --libs libheif libwebp libwebpmux libwebpdemux)"
(cd "$magick" && LIBS="$delegate_libs ${LIBS:-}" ./configure --prefix="$prefix" --disable-shared --enable-static --without-perl --without-x --without-zstd --with-heic=yes --with-raw=yes --with-jpeg=yes --with-png=yes --with-tiff=yes --with-webp=yes && make -j"$jobs" && make install)

extension=""
[ "${RUNNER_OS:-}" = "Windows" ] && extension=".exe"
for binary in ffmpeg ffprobe magick; do
  cp "$prefix/bin/$binary$extension" "$output/bin/"
done
"$output/bin/ffmpeg$extension" -version
"$output/bin/ffprobe$extension" -version
"$output/bin/magick$extension" -version
echo "构建完成：$output"
