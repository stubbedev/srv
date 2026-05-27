#
# Reference copy of the Homebrew formula pushed to
# https://github.com/stubbedev/homebrew-srv on each tagged release.
#
# This file is NOT consumed at install time — the bump-tap job in
# .github/workflows/release.yml regenerates the tap's Formula/srv.rb from
# scratch with the release's version and sha256 hashes. Edit both files
# together when changing formula shape (install layout, service block,
# caveats, etc.).
#
# The 0.0.0 / zeroed sha256 values below are placeholders so this file is
# parseable as-is; do not try to install it directly.
#
class Srv < Formula
  desc "Traefik + TLS + DNS edge layer for static, proxy, and container sites"
  homepage "https://github.com/stubbedev/srv"
  version "0.0.0"
  license "MIT"

  depends_on "mkcert"

  on_macos do
    on_arm do
      url "https://github.com/stubbedev/srv/releases/download/v#{version}/srv-#{version}-darwin-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/stubbedev/srv/releases/download/v#{version}/srv-#{version}-darwin-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/stubbedev/srv/releases/download/v#{version}/srv-#{version}-linux-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
    on_intel do
      url "https://github.com/stubbedev/srv/releases/download/v#{version}/srv-#{version}-linux-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"
    end
  end

  def install
    bin.install "srv"
  end

  # Lets users run `brew services start srv` to keep the watch
  # daemon running across reboots without invoking
  # `srv daemon install` manually. The two installers are
  # mutually exclusive — both register a launchd agent / systemd
  # user unit that runs the same Docker watcher, and would race
  # over container-attach events. The caveats below ask users
  # to pick one.
  service do
    run [opt_bin/"srv", "daemon", "start", "--foreground"]
    # KeepAlive only on unexpected exits — `brew services stop`
    # and `srv daemon stop` (clean exit 0) leave the daemon
    # down. Mirrors the plist `srv daemon install` writes so
    # behaviour matches across install paths.
    keep_alive successful_exit: false
    process_type :background
    log_path var/"log/srv.log"
    error_log_path var/"log/srv.log"
  end

  def caveats
    <<~CAVEATS
      srv needs Docker installed and running.

      To start the watch daemon in the background:
        brew services start srv

      Or, without homebrew-services:
        srv daemon install

      Don't enable both — they register competing launchd / systemd
      units that race over the same Docker watcher.

      First-time setup (Traefik, dnsmasq, mkcert CA, Docker network):
        srv install
    CAVEATS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/srv version")
  end
end
