# Dockerfile — Docker-in-Docker test server that simulates a VPS.
#
# Runs sshd + dockerd inside a single container. vibew deploy can SSH in
# and run docker compose commands against the LOCAL Docker daemon — all
# bind mounts resolve correctly because the daemon and the filesystem
# are on the same machine.
#
# Requires --privileged when run (needed for dockerd).

FROM docker:27-dind

# docker:27-dind already includes docker CLI + compose plugin.
# Upgrade base packages first to resolve version conflicts with openssh,
# then add sshd and utilities.
RUN apk upgrade --no-cache \
    && apk add --no-cache \
    openssh-server \
    bash \
    curl

# Generate host keys for sshd.
RUN ssh-keygen -A

# Create the test user with a known password.
# Add to the 'docker' group so docker CLI works without sudo.
RUN adduser -D -s /bin/bash vibew \
    && echo "vibew:vibew" | chpasswd \
    && addgroup vibew docker \
    && mkdir -p /home/vibew/.ssh \
    && chown -R vibew:vibew /home/vibew \
    && ln -s /usr/local/bin/docker /usr/bin/docker

# Allow password authentication for test simplicity.
RUN sed -i 's/#PasswordAuthentication yes/PasswordAuthentication yes/' /etc/ssh/sshd_config \
    && sed -i 's/#PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config

EXPOSE 22

# Start both dockerd (background) and sshd (foreground).
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
