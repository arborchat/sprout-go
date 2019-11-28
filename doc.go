/*
Package sprout provides types and utilities for implementing client and server programs
that speak the Sprout Protocol. The Sprout Protocol is specified here:

https://man.sr.ht/~whereswaldon/arborchat/specifications/sprout.md

This package exports several important types.

The Conn type wraps a connection-oriented transport (usually a TCP connection)
and provides methods for sending sprout messages and reading sprout messages
off of the connection. It has a number of exported fields which are functions
that should handle incoming messages. These must be set by the user, and their
behavior should conform to the Sprout specification. If using a Conn directly,
be sure to invoke the ReadMessage() method properly to ensure that you receive
repies.

The Worker type wraps a Conn and provides automatic implementations of both the
handler functions for each sprout message and the processing loop that will
read new messages and dispatch their handlers. You can send messages on a worker
by calling Conn methods via struct embedding. It has an exported embedded Conn.

Note: The Send{MessageType}Async methods

The Conn type has both synchronous and asynchronous methods for sending messages.
The synchronous ones block until they recieve a response or their timeout channel
emits a value.

It is recommended to provide a time.Ticker's C field as an easy timeout channel.
As an example, here's the recommended way to invoke SendVersion:

    err := conn.SendVersion(time.NewTicker(time.Second*5).C)

The Async versions of each send operation provide more granular control over
blocking behavior. They return a chan interface{}, but will never send anything
other than a sprout.Status or sprout.Response over that channel. It is safe to
assume that the value will be one of those two.

The Async versions also return a handle for the request called a MessageID. This
can be used to cancel the request in the event that it doesn't have a response
or the response no longer matters. This can be done manually using the Cancel()
method on the Conn type. The synchronous version of each send method handles this
for you, but it must be done manually with the async variant.

An example of the appropriate use of an async method:

    resultChan, messageID, err := conn.SendQueryAsync(ids)
    if err != nil {
        // handle err
    }
    select {
        case data := <-resultChan:
            switch asConcrete := data.(type) {
                case sprout.Status:
                    // handle status
                case sprout.Response:
                    // handle Response
            }
        case <-time.NewTicker(time.Second*5).C:
            conn.Cancel(messageID)
            // handle timeout
    }

*/
package sprout
