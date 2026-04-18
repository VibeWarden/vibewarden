# Dockerfile — lightweight test server that simulates a VPS for deploy tests.
#
# Provides sshd (password auth), docker CLI, and docker compose CLI.
# When the Docker socket is mounted from the host, docker commands work
# inside the container, enabling end-to-end deploy integration tests.

FROM alpine:3.23

RUN apk add --no-cache \
    openssh-server \
    docker-cli \
    docker-cli-compose \
    bash \
    curl

# Generate host keys for sshd.
RUN ssh-keygen -A

# Create the test user with a known password.
RUN adduser -D -s /bin/bash vibew \
    && echo "vibew:vibew" | chpasswd \
    && mkdir -p /home/vibew/.ssh \
    && chown -R vibew:vibew /home/vibew

# Allow password authentication for test simplicity.
RUN sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config \
    && sed -i 's/#PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config

EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
