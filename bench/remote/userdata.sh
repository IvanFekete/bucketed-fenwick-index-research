#!/usr/bin/env bash
# EC2 user-data for one benchmark run. Placeholders (@@NAME@@) are filled by
# the GitHub workflow. The instance terminates itself when done or when the
# watchdog fires (InstanceInitiatedShutdownBehavior=terminate).
set -uxo pipefail
exec > >(tee /var/log/bfi-bench.log) 2>&1

BUCKET=@@BUCKET@@
RUN_ID=@@RUN_ID@@
SUITE=@@SUITE@@
MAX_MIN=@@MAX_MIN@@
GO_VERSION=@@GO_VERSION@@

# Watchdog first: whatever happens below, the machine goes away.
shutdown -h +"$MAX_MIN"

finish() {
  aws s3 cp /var/log/bfi-bench.log "s3://$BUCKET/results/$RUN_ID/userdata.log" || true
  echo "$1" | aws s3 cp - "s3://$BUCKET/results/$RUN_ID/DONE" || true
  shutdown -h now
}
trap 'finish failed' ERR

export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -yq linux-tools-common "linux-tools-$(uname -r)" linux-tools-generic zstd numactl cpufrequtils jq unzip curl
snap install aws-cli --classic || { curl -sSo /tmp/awscli.zip https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip && unzip -q /tmp/awscli.zip -d /tmp && /tmp/aws/install; }

curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
export PATH=/usr/local/go/bin:$PATH HOME=/root GOPATH=/root/go GOFLAGS=-mod=mod

mkdir -p /opt/bfi && cd /opt/bfi
aws s3 cp "s3://$BUCKET/code/$RUN_ID.tar.gz" code.tar.gz
tar xzf code.tar.gz

# Push partial results every 3 minutes, so an overrun or a crash loses at
# most a few minutes of measurements (the first bare-metal run lost all).
mkdir -p /opt/bfi/out
( while sleep 180; do aws s3 sync --quiet /opt/bfi/out "s3://$BUCKET/results/$RUN_ID/" || true; done ) &
# Last sync shortly before the watchdog fires.
( sleep $(( (MAX_MIN - 3) * 60 )); aws s3 sync --quiet /opt/bfi/out "s3://$BUCKET/results/$RUN_ID/"; \
  aws s3 cp /var/log/bfi-bench.log "s3://$BUCKET/results/$RUN_ID/userdata.log"; \
  echo watchdog | aws s3 cp - "s3://$BUCKET/results/$RUN_ID/DONE" ) &

set -e
./bench/remote/prepare_host.sh /opt/bfi/out
./bench/remote/run.sh "$SUITE" "/opt/bfi/out"
aws s3 cp --recursive /opt/bfi/out "s3://$BUCKET/results/$RUN_ID/"
trap - ERR
finish ok
