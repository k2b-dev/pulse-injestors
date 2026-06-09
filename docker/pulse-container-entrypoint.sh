#!/bin/sh
set -eu

run_once() {
  /usr/local/bin/pulse-injestor once
}

schedule() {
  interval="${PULSE_INTERVAL_SECONDS:-60}"
  max_runs="${PULSE_CONTAINER_MAX_RUNS:-0}"
  case "$interval" in
    ''|*[!0-9]*|0)
      echo "invalid PULSE_INTERVAL_SECONDS: $interval" >&2
      exit 2
      ;;
  esac
  case "$max_runs" in
    ''|*[!0-9]*)
      echo "invalid PULSE_CONTAINER_MAX_RUNS: $max_runs" >&2
      exit 2
      ;;
  esac

  stop=0
  trap 'stop=1' INT TERM

  runs=0
  while [ "$stop" -eq 0 ]; do
    runs=$((runs + 1))
    echo "pulse container run $runs starting" >&2
    if ! run_once; then
      echo "pulse container run $runs failed" >&2
    fi
    if [ "$max_runs" -gt 0 ] && [ "$runs" -ge "$max_runs" ]; then
      break
    fi
    sleep "$interval" &
    wait "$!" || true
  done
}

case "${1:-schedule}" in
  schedule)
    shift || true
    if [ "$#" -gt 0 ]; then
      echo "schedule mode does not accept positional arguments" >&2
      exit 2
    fi
    schedule
    ;;
  once|run|--help|-h|--version)
    exec /usr/local/bin/pulse-injestor "$@"
    ;;
  pulse-injestor)
    shift
    exec /usr/local/bin/pulse-injestor "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
