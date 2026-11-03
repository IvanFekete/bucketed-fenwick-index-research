#!/usr/bin/env bash
# Reproduces every simulation result used in the paper (decorrelated arrivals).
# Two lanes run in parallel; outputs go to $OUT (default analysis/out/sim/v2).
set -euo pipefail
BIN=${SIM_BIN:-./sim}
OUT=${OUT:-analysis/out/sim/v2}
mkdir -p "$OUT"
BS=1,2,5,10,15,20,25,30,35,40,50,75,100,150,200
lane_true() {
  $BIN -accept true  -b $BS > "$OUT/grid_true.jsonl" 2> "$OUT/grid_true.log"
  SIM_BIN=$BIN SENS_OUT=$OUT/sens ./analysis/run_sensitivity.sh true
}
lane_bucket() {
  $BIN -accept bucket -b $BS > "$OUT/grid_bucket.jsonl" 2> "$OUT/grid_bucket.log"
  SIM_BIN=$BIN SENS_OUT=$OUT/sens ./analysis/run_sensitivity.sh bucket
}
mkdir -p "$OUT/sens"
lane_true & lane_bucket & wait
echo all-done > "$OUT/ALL_DONE"
