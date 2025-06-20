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

Use the automatic proxy configuration address `http://localhost:999/proxy.pac` in your browser or operating system settings. This will instruct clients to use the proxy servers only for the specific host names configured.

Hit Ctrl + C to stop the program.

## Limitations

- In some cases, linkmeup may cause the opening of several browser tabs for Teleport re-authentication. We still have to investigate if we can avoid this.
