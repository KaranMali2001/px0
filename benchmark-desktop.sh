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
HELPER="$BENCH_TMP/px0-bench-helper-2"
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
  IDLE=10                seconds of idle CPU sampling per launch
  LAUNCHES=3             launches per repo for --app (medians reported)
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
  local src="$HELPER.swift"
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
case "mem":
    // Physical footprint now and at its lifetime peak, in bytes: the kernel
    // counters behind Activity Monitor's Memory column.
    for s in args.dropFirst(2) {
        guard let pid = pid_t(s) else { continue }
        var info = rusage_info_v4()
        let ok = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: rusage_info_t?.self, capacity: 1) { proc_pid_rusage(pid, RUSAGE_INFO_V4, $0) }
        }
        if ok == 0 { print(pid, info.ri_phys_footprint, info.ri_lifetime_max_phys_footprint) }
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

rss_sum_mb() {
  [ $# -gt 0 ] || { echo 0; return; }
  ps -o rss= -p "$(IFS=,; echo "$*")" 2>/dev/null | awk '{s+=$1} END{printf "%.0f", s/1024}'
}

# fp_mb PID... - physical footprint, what Activity Monitor shows as Memory.
# RSS also counts the shared system frameworks mapped into every process,
# which roughly doubles the total for a WebKit app.
fp_mb() {
  [ $# -gt 0 ] || { echo 0; return; }
  "$HELPER" mem "$@" | awk '{s+=$2} END{printf "%.0f", s/1048576}'
}

peak_fp_mb() {
  "$HELPER" mem "$1" | awk '{printf "%.0f", $3/1048576}'
}

# cpu_split SECS APP HELPER... - average %CPU of the app and of its helpers
cpu_split() {
  local secs=$1 app=$2 i list
  shift 2
  list=$(echo "$app $*" | tr -s ' ' ,)
  list=${list%,}
  for i in $(seq "$secs"); do
    ps -o pid=,%cpu= -p "$list" 2>/dev/null
    sleep 1
  done | awk -v app="$app" -v n="$secs" '{ if ($1==app) a+=$2; else w+=$2 } END{printf "%.2f %.2f", a/n, w/n}'
}

biggest_file() {
  python3 - "$1" <<'PY' 2>/dev/null
import os, sys
root = os.path.abspath(sys.argv[1])
exts = {'.go', '.c', '.h', '.py', '.js', '.ts', '.java', '.rs', '.cpp'}
skip = {'.git', 'node_modules', 'vendor', 'dist'}
best, size = '', 0
for d, dirs, files in os.walk(root):
    dirs[:] = [x for x in dirs if x not in skip]
    for f in files:
        if os.path.splitext(f)[1] in exts:
            p = os.path.join(d, f)
            try:
                s = os.path.getsize(p)
            except OSError:
                continue
            if 80000 < s < 8388608 and s > size:
                best, size = os.path.relpath(p, root), s
print(best)
PY
}

urlenc() { printf %s "$1" | sed 's/ /%20/g; s/#/%23/g; s/?/%3F/g'; }

# workload BASE DIR - what a review session does: fuzzy find, full-tree
# searches, then open the largest file and scroll through it.
workload() {
  local base=$1 big i
  curl -s "$base/api/find?q=srv&limit=100" >/dev/null
  for i in 1 2 3; do curl -s "$base/api/search?q=zzqqxx_no_such_token" >/dev/null; done
  big=$(biggest_file "$2")
  [ -n "$big" ] || return 0
  for i in $(seq 0 20); do
    curl -s "$base/api/file?path=$(urlenc "$big")&start=$((i * 1000))&count=1000" >/dev/null
  done
}

LAUNCHES=${LAUNCHES:-3}
APP_ROWS=""

app_once() {
  local dir=$1 name=$2 run=$3 port=$PORT pid t0 t_win="" t_ready="" i
  t0=$(now_ms)
  open_app "$port" 1 "$dir" || return 1
  pid=$(app_pid "$port") || return 1
  for i in $(seq 1500); do
    if [ -z "$t_win" ] && [ "$("$HELPER" windows "$pid")" -gt 0 ]; then t_win=$(( $(now_ms) - t0 )); fi
    if [ -z "$t_ready" ] && curl -sf "http://127.0.0.1:$port/api/meta" 2>/dev/null | grep -q '"ready":true'; then
      t_ready=$(( $(now_ms) - t0 ))
    fi
    [ -n "$t_win" ] && [ -n "$t_ready" ] && break
    sleep 0.02
  done

  # Let the page finish loading and the post-index git status settle.
  sleep 5
  local helpers app_rss web_rss app_fp web_fp cpu work_fp peak idle_fp web_idle_fp
  # shellcheck disable=SC2046
  helpers=$("$HELPER" responsible "$pid" $(ps -axo pid=,comm= | awk '/com\.apple\.WebKit\./{print $1}'))
  app_rss=$(rss_sum_mb "$pid")
  # shellcheck disable=SC2086
  web_rss=$(rss_sum_mb $helpers)
  app_fp=$(fp_mb "$pid")
  # shellcheck disable=SC2086
  web_fp=$(fp_mb $helpers)
  # shellcheck disable=SC2086
  cpu=$(cpu_split "$IDLE" "$pid" $helpers)

  workload "http://127.0.0.1:$port" "$dir"
  work_fp=$(fp_mb "$pid")
  peak=$(peak_fp_mb "$pid")
  sleep 30
  idle_fp=$(fp_mb "$pid")
  # shellcheck disable=SC2086
  web_idle_fp=$(fp_mb $helpers)

  APP_ROWS="$APP_ROWS$name,$run,${t_win:-},${t_ready:-},$app_rss,$web_rss,$app_fp,$web_fp,${cpu% *},${cpu#* },$work_fp,$peak,$idle_fp,$web_idle_fp
"
  stop_app "$pid"
  PORT=$((port + 1))
}

bench_app() {
  local targets=("$@") t dir run
  if [ ${#targets[@]} -eq 0 ]; then
    [ -d "$CORPUS" ] || die "no corpus; run: $0 --clone"
    targets=("$CORPUS"/*/)
  fi
  build_helper
  for t in "${targets[@]}"; do
    dir=$(cd "${t%/}" 2>/dev/null && pwd) || { echo "  skip $t (missing)"; continue; }
    for run in $(seq "$LAUNCHES"); do
      app_once "$dir" "$(basename "$dir")" "$run" || echo "  skip $(basename "$dir") run $run (did not launch)"
    done
  done
  local header="repo,run,windowMs,readyMs,appRssMB,webkitRssMB,appFootprintMB,webkitFootprintMB,appIdleCpuPct,webkitIdleCpuPct,appAfterWorkMB,appPeakMB,appIdle30sMB,webkitIdle30sMB"
  local csv="$BENCH_TMP/px0-bench-app.csv"
  printf '%s\n%s' "$header" "$APP_ROWS" >"$csv"
  python3 - "$csv" <<'PY'
import csv, statistics, sys
rows = list(csv.DictReader(open(sys.argv[1])))
def med(rs, k):
    v = [float(r[k]) for r in rs if r[k]]
    return statistics.median(v) if v else float("nan")
print()
print("| Repo         | Window  | Ready   | Memory: app + WebKit = total | RSS total | Idle CPU app / WebKit | After work | Peak    | Settled: app + WebKit = total |")
print("| ------------ | ------- | ------- | ---------------------------- | --------- | --------------------- | ---------- | ------- | ----------------------------- |")
for name in dict.fromkeys(r["repo"] for r in rows):
    rs = [r for r in rows if r["repo"] == name]
    m = lambda k: med(rs, k)
    a, w = m("appFootprintMB"), m("webkitFootprintMB")
    rss = m("appRssMB") + m("webkitRssMB")
    print(f"| {name:<12} | {m('windowMs'):4.0f} ms | {m('readyMs'):4.0f} ms "
          f"| {a:4.0f} + {w:3.0f} = {a + w:4.0f} MB         | {rss:6.0f} MB "
          f"| {m('appIdleCpuPct'):5.2f}% / {m('webkitIdleCpuPct'):4.2f}%        "
          f"| {m('appAfterWorkMB'):6.0f} MB  | {m('appPeakMB'):4.0f} MB | {m('appIdle30sMB'):4.0f} + {m('webkitIdle30sMB'):3.0f} = {m('appIdle30sMB') + m('webkitIdle30sMB'):4.0f} MB          |")
PY
  echo
  echo "Median of $LAUNCHES launches. Window/Ready: from the launch request. Memory: physical footprint"
  echo "(Activity Monitor's Memory column); WebKit = WebContent + Networking + GPU helpers. Idle CPU:"
  echo "${IDLE}s average, window in the background, language servers off. After work / Peak: app"
  echo "footprint after fuzzy find, 3 full-tree searches and scrolling the largest file; Settled: 30 s later."
  if [ -n "${APP_CSV:-}" ]; then
    cp "$csv" "$APP_CSV"
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
