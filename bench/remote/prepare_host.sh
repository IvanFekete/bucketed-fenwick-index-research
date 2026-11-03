#!/usr/bin/env bash
# Put the host into a reproducible benchmarking state and record it.
# Every step is best-effort; what actually took effect is written to
# host.json so results are self-describing.
set -uo pipefail
OUT=${1:-/opt/bfi/out}
mkdir -p "$OUT"

sysctl -w kernel.perf_event_paranoid=-1 >/dev/null 2>&1
sysctl -w kernel.kptr_restrict=0 >/dev/null 2>&1

# Fixed frequency: performance governor, turbo/boost off.
for g in /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor; do echo performance > "$g" 2>/dev/null; done
echo 1 > /sys/devices/system/cpu/intel_pstate/no_turbo 2>/dev/null
echo 0 > /sys/devices/system/cpu/cpufreq/boost 2>/dev/null

# One hardware thread per core.
echo off > /sys/devices/system/cpu/smt/control 2>/dev/null

# Transparent huge pages: fixed to madvise (Go runtime default behaviour).
echo madvise > /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null

systemctl stop irqbalance 2>/dev/null
swapoff -a 2>/dev/null

perf_ok=false
if perf stat -e cycles,instructions,LLC-loads,LLC-load-misses -- true >/dev/null 2>&1; then perf_ok=true; fi

jq -n \
  --arg lscpu "$(lscpu)" \
  --arg numa "$(numactl --hardware 2>/dev/null)" \
  --arg kernel "$(uname -a)" \
  --arg go "$(go version 2>/dev/null)" \
  --arg governor "$(cat /sys/devices/system/cpu/cpu0/cpufreq/scaling_governor 2>/dev/null)" \
  --arg no_turbo "$(cat /sys/devices/system/cpu/intel_pstate/no_turbo 2>/dev/null)" \
  --arg smt "$(cat /sys/devices/system/cpu/smt/control 2>/dev/null)" \
  --arg thp "$(cat /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null)" \
  --arg microcode "$(grep -m1 microcode /proc/cpuinfo)" \
  --arg instance "$(T=$(curl -s -m 2 -X PUT http://169.254.169.254/latest/api/token -H 'X-aws-ec2-metadata-token-ttl-seconds: 60'); curl -s -m 2 -H "X-aws-ec2-metadata-token: $T" http://169.254.169.254/latest/meta-data/instance-type)" \
  --argjson perf "$perf_ok" \
  '{lscpu:$lscpu, numa:$numa, kernel:$kernel, go:$go, governor:$governor, no_turbo:$no_turbo,
    smt:$smt, thp:$thp, microcode:$microcode, instance_type:$instance, perf_counters:$perf}' > "$OUT/host.json"
cat "$OUT/host.json"
