#!/usr/bin/env bash
# Benchmark the px0 desktop app with the same suite as benchmark.sh.
#
# benchmark.sh measures a server over HTTP. Here that server is the real app:
# each run launches px0.app through LaunchServices with PX0_BENCH_PORT set, so
# the in-process workspace server also answers on loopback and every number
# (index, fuzzy, scan, open, memory) comes from the desktop process itself.
# --app adds what only the desktop app has: window time, WebKit helper
# processes and idle CPU.
set -u

HERE=$(cd "$(dirname "$0")" && pwd)
APP=${APP:-$HERE/build/bin/px0.app}
BENCH_TMP="${TMPDIR:-/tmp}"
BENCH_TMP="${BENCH_TMP%/}"
# A private copy with its own bundle id and HOME, so runs never touch the
# installed app's settings, window state, recent folders or WebKit storage.
BENCH_APP="$BENCH_TMP/px0-bench.app"
BENCH_HOME="$BENCH_TMP/px0-bench-home"
BENCH_EXE="$BENCH_APP/Contents/MacOS/px0"
HELPER="$BENCH_TMP/px0-bench-helper"
IDLE=${IDLE:-10}

die() { echo "benchmark-desktop: $*" >&2; exit 1; }

now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }

usage() {
  cat <<'EOF'
usage: ./benchmark-desktop.sh [mode] [options] [directory ...]

Runs benchmark.sh against the desktop app, then adds desktop-only numbers.

modes:
  (none)            benchmark.sh table for the app, then the --app table
  --app             desktop-only: launch to window, index ready, app and
                    WebKit memory, idle CPU
  --clone, --memory, --lsp, --load, --json, --csv, --compare, --micro
                    same as benchmark.sh, measured on the desktop app
  --help            show this

environment:
  APP=build/bin/px0.app  app bundle to measure (run: make desktop)
  CORPUS, PORT, RUNS     as in benchmark.sh
  IDLE=10                seconds of idle CPU sampling per repo
  FOREGROUND=1           bring the window to the front (default: background,
                         which also selects the git watcher's background policy)

Keep the corpus outside Desktop, Documents and Downloads, or macOS asks for
folder access on every launch.
EOF
}

open_app() {
  local port=$1 nolsp=$2 dir=$3 bg=-g
  [ "${FOREGROUND:-}" = 1 ] && bg=
  open -n $bg --env HOME="$BENCH_HOME" --env PATH="$PATH" \
    --env PX0_BENCH_PORT="$port" --env PX0_BENCH_NO_LSP="$nolsp" \
    "$BENCH_APP" --args "$dir" || return 1
}

# app_pid PORT - the bench app instance listening on PORT
app_pid() {
  local i pid
  for i in $(seq 100); do
    pid=$(lsof -nP -t -iTCP:"$1" -sTCP:LISTEN 2>/dev/null | head -1)
    if [ -n "$pid" ] && ps -o args= -p "$pid" | grep -qF "$BENCH_EXE"; then
      echo "$pid"; return 0
    fi
    sleep 0.1
  done
  return 1
}

stop_app() {
  local pid=$1 i
  kill -TERM "$pid" 2>/dev/null || return 0
  for i in $(seq 100); do
    kill -0 "$pid" 2>/dev/null || return 0
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}

# Shim mode: benchmark.sh runs "$BIN -no-open -port N [flags] DIR" and later
# kills that pid. Stand in for the CLI: launch the app, then live as long as
# it does and pass the kill on.
shim() {
  local port="" nolsp=0 dir="" prev=""
  for a in "$@"; do
    case "$prev" in -port) port=$a; prev=""; continue ;; esac
    case "$a" in
      -port) prev=-port ;;
      -no-lsp) nolsp=1 ;;
      -*) ;;
      *) dir=$a ;;
    esac
  done
  [ -n "$port" ] && [ -n "$dir" ] || die "shim: need -port and a directory"
  open_app "$port" "$nolsp" "$dir" || exit 1
  local pid
  pid=$(app_pid "$port") || exit 1
  trap 'stop_app "$pid"; exit 0' TERM INT
  while kill -0 "$pid" 2>/dev/null; do sleep 0.2; done
}

if [ "${PX0_DESKTOP_SHIM:-}" = 1 ]; then
  shim "$@"
  exit 0
fi

build_helper() {
  [ -x "$HELPER" ] && return 0
  command -v swiftc >/dev/null || die "swiftc is required (xcode-select --install)"
  local src="$BENCH_TMP/px0-bench-helper.swift"
  cat >"$src" <<'SWIFT'
import CoreGraphics
import Foundation

let args = CommandLine.arguments
switch args[1] {
case "windows":
    // On-screen normal windows owned by the pid.
    let pid = Int32(args[2])!
    let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
    print(list.filter { ($0[kCGWindowOwnerPID as String] as? Int32) == pid && ($0[kCGWindowLayer as String] as? Int) == 0 }.count)
case "responsible":
    // WebKit helpers are XPC services parented to launchd; the responsible
    // pid is what ties them to the app (Activity Monitor groups by it too).
    typealias Fn = @convention(c) (pid_t) -> pid_t
    guard let sym = dlsym(dlopen(nil, RTLD_NOW), "responsibility_get_pid_responsible_for_pid") else { exit(1) }
    let responsible = unsafeBitCast(sym, to: Fn.self)
    let target = pid_t(args[2])!
    for s in args.dropFirst(3) {
        if let p = pid_t(s), p != target, responsible(p) == target { print(p) }
    }
default:
    exit(2)
}
SWIFT
  swiftc -O -o "$HELPER" "$src" 2>/dev/null || die "could not build $HELPER"
}

prepare_app() {
  [ -d "$APP" ] || die "$APP not found; run: make desktop"
  rm -rf "$BENCH_APP"
  ditto "$APP" "$BENCH_APP"
  plutil -replace CFBundleIdentifier -string com.wails.px0.bench "$BENCH_APP/Contents/Info.plist"
  codesign --force --deep -s - "$BENCH_APP" >/dev/null 2>&1 || die "could not sign $BENCH_APP"
  mkdir -p "$BENCH_HOME"
}

helper_pids() {
  local candidates
  candidates=$(ps -axo pid=,comm= | awk '/com\.apple\.WebKit\./{print $1}')
  [ -n "$candidates" ] || return 0
  # shellcheck disable=SC2086
  "$HELPER" responsible "$1" $candidates
}

rss_sum_mb() {
  [ $# -gt 0 ] || { echo 0; return; }
  ps -o rss= -p "$(IFS=,; echo "$*")" 2>/dev/null | awk '{s+=$1} END{printf "%.0f", s/1024}'
}

cpu_avg() {
  local secs=$1; shift
  local list i
  list=$(IFS=,; echo "$*")
  for i in $(seq "$secs"); do
    ps -o %cpu= -p "$list" 2>/dev/null | awk '{s+=$1} END{print s+0}'
    sleep 1
  done | awk '{s+=$1;n++} END{printf "%.2f", (n ? s/n : 0)}'
}

APP_RESULTS=""

app_one() {
  local dir name port=$PORT pid t0 t_win="" t_ready="" i
  dir=$(cd "$1" 2>/dev/null && pwd) || { echo "  skip $1 (missing)"; return; }
  name=$(basename "$dir")
  t0=$(now_ms)
  open_app "$port" 1 "$dir" || { echo "  skip $name (did not launch)"; return; }
  pid=$(app_pid "$port") || { echo "  skip $name (no app process)"; return; }
  for i in $(seq 600); do
    if [ -z "$t_win" ] && [ "$("$HELPER" windows "$pid")" -gt 0 ]; then t_win=$(( $(now_ms) - t0 )); fi
    if [ -z "$t_ready" ] && curl -sf "http://127.0.0.1:$port/api/meta" 2>/dev/null | grep -q '"ready":true'; then
      t_ready=$(( $(now_ms) - t0 ))
    fi
    [ -n "$t_win" ] && [ -n "$t_ready" ] && break
    sleep 0.02
  done

  # Let the page finish loading and the post-index git status settle.
  sleep 5
  local helpers app_mb web_mb total cpu
  helpers=$(helper_pids "$pid")
  app_mb=$(rss_sum_mb "$pid")
  # shellcheck disable=SC2086
  web_mb=$(rss_sum_mb $helpers)
  total=$((app_mb + web_mb))
  # shellcheck disable=SC2086
  cpu=$(cpu_avg "$IDLE" "$pid" $helpers)

  printf '| %-12s | %8s | %8s | %7s | %7s | %8s | %3s | %7s |\n' \
    "$name" "${t_win:-?} ms" "${t_ready:-?} ms" "$app_mb MB" "$web_mb MB" "$total MB" \
    "$(echo "$helpers" | grep -c .)" "$cpu%"
  APP_RESULTS="$APP_RESULTS$name,${t_win:-},${t_ready:-},$app_mb,$web_mb,$total,$cpu
"
  stop_app "$pid"
  PORT=$((port + 1))
}

bench_app() {
  local targets=("$@")
  if [ ${#targets[@]} -eq 0 ]; then
    [ -d "$CORPUS" ] || die "no corpus; run: $0 --clone"
    targets=("$CORPUS"/*/)
  fi
  build_helper
  echo
  echo "| Repo         | Window   | Ready    | App     | WebKit  | Total    | Hlp | Idle CPU |"
  echo "| ------------ | -------- | -------- | ------- | ------- | -------- | --- | -------- |"
  for t in "${targets[@]}"; do app_one "${t%/}"; done
  echo
  echo "Window/Ready: from launch request. App: px0 process RSS. WebKit: its WebContent,"
  echo "Networking and GPU helpers. Idle CPU: app + helpers, ${IDLE}s average, language servers off."
  if [ -n "${APP_CSV:-}" ]; then
    { echo "repo,windowMs,readyMs,appMB,webkitMB,totalMB,idleCpuPct"; printf %s "$APP_RESULTS"; } >"$APP_CSV"
    echo "Exported desktop results to $APP_CSV"
  fi
}

if [ -z "${CORPUS:-}" ]; then
  if [ -d "$HERE/bench-repos" ]; then CORPUS="$HERE/bench-repos"; else CORPUS="$HERE/../bench-repos"; fi
fi
export CORPUS
PORT=${PORT:-7900}

case "${1-}" in
  --help|-h) usage; exit 0 ;;
  --clone|--micro) exec "$HERE/benchmark.sh" "$@" ;;
  --vscode|--vscode-vanilla|--editors) die "$1 compares the CLI server; use ./benchmark.sh $1" ;;
esac

prepare_app
trap 'pkill -f "$BENCH_EXE" 2>/dev/null || true' EXIT
export PX0_DESKTOP_SHIM=1 BIN="$HERE/benchmark-desktop.sh" PORT

case "${1-}" in
  --app) shift; unset PX0_DESKTOP_SHIM; bench_app "$@"; exit 0 ;;
  -*)    "$HERE/benchmark.sh" "$@"; exit $? ;;
esac

(cd "$HERE" && ./benchmark.sh "$@") || exit $?
unset PX0_DESKTOP_SHIM
PORT=$((PORT + 100))
bench_app "$@"
