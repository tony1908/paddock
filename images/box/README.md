# Paddock box image

`paddock/box` is a LinuxServer Webtop XFCE desktop image containing the `boxd`
agent, browser, coding harnesses, SSH access, and optional Docker-in-Docker for
Sysbox-backed boxes.

Build it from the repository root:

```sh
docker build -f images/box/Dockerfile -t paddock/box:dev .
```

Run it with a per-box `BOXD_TOKEN` and LinuxServer desktop password:

```sh
docker run -d --name my-box -e BOXD_TOKEN=replace-me -e PASSWORD=replace-me paddock/box:dev
```

`boxd serve` listens on port 7070 and requires `BOXD_TOKEN`. The image starts
`boxd setup` asynchronously at container initialization. SSH listens on port
22 and accepts public keys from `/config/.ssh/authorized_keys`; password
authentication is disabled. The Docker daemon is enabled only when the
container is running in a user namespace (such as Sysbox), and otherwise its
service remains idle. Once started, dockerd stops after 15 idle minutes (no
running containers) and restarts on the next docker call.

The desktop is off at boot and starts on demand: the agent runs
`paddock-desktop start` when someone opens the desktop, runs `exec --desktop`,
or takes a screenshot. It stops after 10 idle minutes (no connected client and
no open windows); set `PADDOCK_DESKTOP_IDLE_MINUTES` to change this, or `0` to
never stop it. Set `PADDOCK_HEADLESS=1` to disable the desktop entirely, and
`PADDOCK_RESOLUTION=WxH` (for example `1280x800`) to set its size. These values
are read from `/run/paddock/env`, falling back to the container environment.
Chrome runs with low-memory flags through the `paddock-browser` wrapper.

Firefox is installed on amd64 and arm64. Google Chrome is additionally
installed on amd64. Selkies uses a fixed 1920×1080 desktop resolution.

The XFCE session is a custom Failsafe session (`root/etc/xdg/xfce4/xfconf/xfce-perchannel-xml/xfce4-session.xml`) that runs only xfwm4, xfsettingsd and xfce4-panel; xfdesktop (desktop icons and wallpaper) and Thunar are not started.
