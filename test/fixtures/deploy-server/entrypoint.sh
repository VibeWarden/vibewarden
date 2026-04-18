#!/bin/sh
set -e

# Start Docker daemon in the background.
# --storage-driver=overlay2 is the most compatible option.
dockerd --storage-driver=overlay2 --iptables=false &

# Wait for Docker daemon to be ready (up to 30s).
timeout=30
while ! docker info >/dev/null 2>&1; do
    timeout=$((timeout - 1))
    if [ "$timeout" -le 0 ]; then
        echo "ERROR: dockerd failed to start within 30s" >&2
        exit 1
    fi
    sleep 1
done

echo "Docker daemon ready"

# Start sshd in the foreground.
exec /usr/sbin/sshd -D -e
