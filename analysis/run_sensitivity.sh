#!/usr/bin/env bash
# One-at-a-time sensitivity of the C2 simulation to the matchmaker parameters.
# Shard 0.1, policy "best"; defaults delta0=25, growth=5, k=16.
set -euo pipefail
ACCEPT=$1
BIN=${SIM_BIN:-./sim}
OUT=${SENS_OUT:-analysis/out/sim/sens}
BS=1,5,10,25,35,50,100
for d0 in 10 25 50 100; do
  $BIN -accept "$ACCEPT" -shard 0.1 -policy best -b $BS -delta0 $d0 >> $OUT/${ACCEPT}_delta0.jsonl 2>> $OUT/${ACCEPT}.log
done
for g in 2 10; do
  $BIN -accept "$ACCEPT" -shard 0.1 -policy best -b $BS -growth $g >> $OUT/${ACCEPT}_growth.jsonl 2>> $OUT/${ACCEPT}.log
done
for k in 4 64; do
  $BIN -accept "$ACCEPT" -shard 0.1 -policy best -b $BS -k $k >> $OUT/${ACCEPT}_k.jsonl 2>> $OUT/${ACCEPT}.log
done
