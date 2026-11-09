# linkmeup

Linkmeup ("_link-me-up_") is a tool to help you access web applications (HTTPS protocol) in private Giant Swarm installations, with the help of Teleport.

## Prerequisites

1. You need `tsh` installed. [Installation instructions](https://goteleport.com/docs/connect-your-client/tsh/#installing-tsh)
2. You must have logged in via `tsh login --auth ... --proxy ... CLUSTER`. Giant Swarm users find the correct command in the [intranet](https://intranet.giantswarm.io/docs/support-and-ops/teleport/web-access/).

## Configuration

Linkmeup requires a config file. It will look for a file called `linkmeup.yaml` in `$HOME/.config` and in the current working directory. Look at `linkmeup.example.yaml`for an explanation of the format.

Giant Swarm users find the latest config in the [intranet](https://intranet.giantswarm.io/docs/support-and-ops/teleport/web-access/#linkmeup).

## Installation

With Go installed, you can install the tool like this:

```bash
go install github.com/giantswarm/linkmeup@latest
```

## Usage

Simply run `linkmeup` in the terminal.

In the terminal user interface, navigate the list of installations with `↑`/`↓` (or `k`/`j`) and hit `Enter` to see details for the selected installation, including the reason why its proxy is unhealthy. Hit `Enter` or `Esc` to get back to the list, and `q` to quit.

Use the automatic proxy configuration address `http://localhost:9999/proxy.pac` in your browser or operating system settings. This will instruct clients to use the proxy servers only for the specific host names configured.

Hit Ctrl + C to stop the program.

Run `linkmeup version` to print the version, commit and build time of the binary, or `linkmeup --version`. Neither needs a config file.

## Headless mode

Run without the terminal UI in scripts, services, or agent sessions:

```bash
linkmeup --no-tui --config /path/to/linkmeup.yaml
```

Headless mode is selected automatically when standard input is not a terminal,
including when it is redirected from `/dev/null`. On a terminal the UI remains
the default. Headless mode writes plain, line-oriented logs to standard output:
installation names and ports, health changes with failure reasons, and tunnel
restart attempts (which can trigger Teleport re-login). `--log-level` controls
the verbosity. Teleport login is still required before starting.

The SOCKS5 proxies and `http://localhost:9999/proxy.pac` work in either mode.
Send SIGINT (Ctrl+C) or SIGTERM to stop the PAC server, health checks, and tunnels
cleanly.

## Limitations

- A tunnel that cannot be established makes `tsh` attempt Teleport re-authentication, which opens a browser tab. linkmeup now spaces restart attempts out, up to 15 minutes apart, so a broken installation produces a handful of tabs an hour rather than a stream of them. Suppressing them entirely needs a `tsh` flag that currently does not work (`--no-relogin` consumes the target host in Teleport v18).
