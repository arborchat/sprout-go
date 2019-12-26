# sprout-go

[![builds.sr.ht status](https://builds.sr.ht/~whereswaldon/sprout-go.svg)](https://builds.sr.ht/~whereswaldon/sprout-go?)
[![GoDoc](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go?status.svg)](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go)

This is an implementation of the [Sprout Protocol](https://man.sr.ht/~whereswaldon/arborchat/specifications/sprout.md) in Go. It provides methods
to send and receive protocol messages in Sprout. Sprout is one part of the
Arbor Chat project.

## About Arbor

![arbor logo](https://git.sr.ht/~whereswaldon/forest-go/blob/master/img/arbor-logo.png)

Arbor is a chat system that makes communication clearer. It explicitly captures context that other platforms ignore, allowing you to understand the relationship between each message and every other message. It also respects its users and focuses on group collaboration.

You can get information about the Arbor project [here](https://man.sr.ht/~whereswaldon/arborchat/).

For news about the project, join our [mailing list](https://lists.sr.ht/~whereswaldon/arbor-dev)!

## Relay

This repo currently contains an example implementation of an Arbor Relay, which is analagous to a "server" in a traditional chat system. You can find it in `cmd/relay`.

### Relay Snap

In making the use of this relay more approachable, we've developed a `snap`. Long term, we will have CI/CD for this snap to prevent users/devs from having to build the snap themselves. Once complete, you'll be able to install from the snap store with `snap install arbor-relay`.

Using this snap will:

- Generate certificates on install: `/var/snap/arbor-relay/common/[cert|key].pem`
- Allow you to run an Arbor relay: `arbor-relay.relay $RELAY_ARGS`

The certificates generated from the snap installation are provided as a convenience, but are not explicitly required to run the relay. You'll have to bring your own certs to the table if you don't want to use ours.

### Developers Building the Relay Snap

Checkout the [getting started](https://snapcraft.io/docs/getting-started) section of the Snapcraft documentation and familiarize yourself with [snapping Go applications](https://snapcraft.io/#go).

Once you have `snap`, `snapcraft`, and `multipass` installed you can build the snap in one of the following ways:

```sh
# Build from the base directory
snapcraft

# Or, build with --debug to open a shell in the VM/container
# if a part of the build fails.
snapcraft --debug

# Or, use LXD container instead of multipass VMs
snapcraft --use-lxd
```

You'll now see a `.snap` file.

### Relay Developer Snap Installation

Install this as follows:

> Note the `--dangerous` flag here just tells snap
> that "this isn't signed by the snap store and
> that's okay."

```sh
snap install --dangerous arbor-relay*.snap
```

### Running the Relay Snap

You'll have `arbor-relay.realy` in your path now. I know, typing that will get a little annoying... This is short-term, but you may alias this within snap to make it more convenient in the meantime:

```sh
snap alias arbor-relay.relay relay
```

### Removing the Snap

Simply `snap remove arbor-relay` when it's no longer needed.
