# sprout-go

[![builds.sr.ht status](https://builds.sr.ht/~whereswaldon/sprout-go.svg)](https://builds.sr.ht/~whereswaldon/sprout-go?)
[![GoDoc](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go?status.svg)](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go)

This is an implementation of the [Sprout Protocol](https://man.sr.ht/~whereswaldon/arborchat/specifications/sprout.md#response) in Go. It provides methods
to send and receive protocol messages in Sprout. Sprout is one part of the
Arbor Chat project.

## About Arbor

![arbor logo](https://git.sr.ht/~whereswaldon/forest-go/blob/master/img/arbor-logo.png)

Arbor is a chat system that makes communication clearer. It explicitly captures context that other platforms ignore, allowing you to understand the relationship between each message and every other message. It also respects its users and focuses on group collaboration.

You can get information about the Arbor project [here](https://man.sr.ht/~whereswaldon/arborchat/).

For news about the project, join our [mailing list](https://lists.sr.ht/~whereswaldon/arbor-dev)!

## Relay

This repo currently contains an example implementation of an Arbor Relay, which is analagous to a "server" in a traditional chat system. You can find it in `cmd/relay`.
