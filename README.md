# sprout-go

[![builds.sr.ht status](https://builds.sr.ht/~whereswaldon/sprout-go.svg)](https://builds.sr.ht/~whereswaldon/sprout-go?)
[![GoDoc](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go?status.svg)](https://godoc.org/git.sr.ht/~whereswaldon/sprout-go)

This is an implementation of the [Sprout Protocol](https://man.sr.ht/~whereswaldon/arborchat/specifications/sprout.md) in Go. It provides methods
to send and receive protocol messages in Sprout. Sprout is one part of the
Arbor Chat project.

> NOTE: this package requires using a fork of golang.org/x/crypto, and you must therefore include the following in your `go.mod`:
> ```
>     replace golang.org/x/crypto => github.com/ProtonMail/crypto <version-from-sprout-go's-go.mod>
> ```

## About Arbor

![arbor logo](https://git.sr.ht/~whereswaldon/forest-go/blob/master/img/arbor-logo.png)

Arbor is a chat system that makes communication clearer. It explicitly captures context that other platforms ignore, allowing you to understand the relationship between each message and every other message. It also respects its users and focuses on group collaboration.

You can get information about the Arbor project [here](https://man.sr.ht/~whereswaldon/arborchat/).

For news about the project, join our [mailing list](https://lists.sr.ht/~whereswaldon/arbor-dev)!

## Relay

This repo currently contains an example implementation of an Arbor Relay, which is analagous to a "server" in a traditional chat system. You can find it in `cmd/relay`.

### Local testing

If you want to run a local relay to test something, it's very easy!

First, install [`mkcert`](https://github.com/FiloSottile/mkcert) so that you can create trusted local TLS certificates.

Then do the following:

```sh
mkcert --install # configure your local CA
mkcert localhost 127.0.0.1 ::1 arbor.local # generate a trusted cert for local addresses
mkdir grove # create somewhere to store arbor forest data

# Copy any nodes you want the relay to have into this grove directory.
# To copy from your local sprig installation:
cp ~/.config/sprig/grove/* grove/
# Adapt this as necessary if your history is stored elsewhere.

relay -certpath ./localhost+3.pem -keypath ./localhost+3-key.pem -grovepath ./grove/
```

You can now connect to this relay with any client on the address `localhost:7777`, `arbor.local:7777`, and the other names that you configured in the certificate.

### Writing sprout by hand

If you want to examine the behavior of a sprout relay, it is sometimes convenient to directly connect to the relay.

To do this, install [`socat`](http://www.dest-unreach.org/socat/) (it's probably in your package manager). If you want to test against the official arbor relay you can do:

```sh
socat openssl:arbor.chat:7117 stdio
```

You can then type valid [sprout protocol messages](https://man.sr.ht/~whereswaldon/arborchat/specifications/sprout.md) and see the relay's responses.

Here's an example session that you could replicate:

```
version 1 0.0
```

Response:

```
status 1 0
```

We advertise our protocol version as 0.0 and the relay agrees to use that version.


```
list 2 1 3
```

Response:

```
response 2 1
SHA512_B32__mw9nEYu_XAgnw0mRRNO60J2bqIOOBmalIXgOqxoKV-o <base64url-node-data>
```

We request a list of the three most recent communities. We only get one back because the relay currently only knows about one.


```
leaves_of 3 SHA512_B32__mw9nEYu_XAgnw0mRRNO60J2bqIOOBmalIXgOqxoKV-o 3
```

Response:

```
response 3 3
SHA512_B32__S7wybDxauEZEYJCRenemN2WB5woupuHlRU-Gj5eVU8M <base64url-node-data>
SHA512_B32__GITqdhoPqbECv6Sb03zW8H7Ry0M8dScmQwMcrdeoEwI <base64url-node-data>
SHA512_B32___wCB6e6y9cWhjdHGyI4ki-qGa-GVWTmgP8ZtYpIK7O0 <base64url-node-data>
```

We request three leaves of the one community that exists.
