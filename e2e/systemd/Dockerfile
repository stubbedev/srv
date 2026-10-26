# A disposable machine with systemd as PID 1 for the daemon-service suite:
# `srv daemon install|uninstall|start|stop` talk to a real `systemctl --user`
# here instead of the host's. The dev user (uid 1000) lingers, so its user
# manager starts at boot without a login session.
FROM debian:trixie-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus dbus-user-session \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -m -u 1000 dev \
 && mkdir -p /var/lib/systemd/linger \
 && touch /var/lib/systemd/linger/dev

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
