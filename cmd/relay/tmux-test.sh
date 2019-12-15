#!/bin/bash

# This script sets up four relays connected together in a star topology. It
# does not generate history, so it needs to be supplied with a path to an
# existing grove that it can copy nodes from. It is mostly useful for e2e testing
# relays.

set -euo pipefail

if ! command -v tmux; then
    echo "This test script requires tmux. Please install it and try again"
    exit 1
fi

if [ -z "$1" ]; then
    echo "Usage: $0 <arbor-history-dir>"
    exit 1
fi

# ensure we're running in tmux
if [ -z "$TMUX" ]; then
    # shellcheck disable=SC2068
    exec tmux -c $@
fi

build_dir=$(dirname "$(realpath "$0")")
relay_executable=$(mktemp)

echo "building relay..."
(cd "$build_dir" && go build -o "$relay_executable")

echo "generating keys..."
keys_dir=$(mktemp -d)
(cd "$keys_dir" && openssl req \
    -x509 \
    -subj '/CN=test.com/O=Testing/C=US' \
    -newkey rsa:4096 \
    -keyout key.pem \
    -out cert.pem \
    -days 365 \
    -nodes)

central_relay_dir=$(mktemp -d)
peer1_relay_dir=$(mktemp -d)
peer2_relay_dir=$(mktemp -d)
peer3_relay_dir=$(mktemp -d)

function cleanup() {
    echo "Cleaning up"
    rm -rf "$keys_dir" "$central_relay_dir" "$peer1_relay_dir" "$peer2_relay_dir" "$peer3_relay_dir"
}

trap cleanup EXIT

start_port=10345
current_port=$start_port

echo "copying nodes from source..."
cp "$1"/* "$central_relay_dir"

echo "launching relays..."
window_name="arbor-relay-testing"
echo "launching central relay from $central_relay_dir"
tmux new-window -c "$central_relay_dir" -n "$window_name" "$relay_executable" \
    -certpath "$keys_dir/cert.pem" \
    -keypath "$keys_dir/key.pem" \
    -grovepath "$central_relay_dir" \
    -tls-port "$current_port"

for i in $peer1_relay_dir $peer2_relay_dir $peer3_relay_dir; do
    echo "Launching peer relay from $i"
    ((current_port++))
    tmux split-window -c "$i" "$relay_executable" \
        -certpath "$keys_dir/cert.pem" \
        -keypath "$keys_dir/key.pem" \
        -grovepath "$i" \
        -tls-port "$current_port" \
        -insecure \
        localhost:"$start_port"
done

echo "press control-c when you're done testing; make sure to kill the relays too"
read -r
