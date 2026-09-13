# Snapshot subscriptions

Snapshot subscriptions are an opt-in extension for automation clients that need
full semantic snapshots after state changes. They do not change Stave protocol
version `1.0`, the existing `stave.snapshot` polling request, or any existing
v1 payload.

## Negotiate the extension

A client offers the extension during `stave.initialize`:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "stave.initialize",
  "params": {
    "protocolVersions": ["1.0"],
    "capabilities": {
      "snapshotSubscriptionVersions": ["stave.snapshot.subscribe/v1"]
    }
  }
}
```

The offer alone does not enable subscriptions. The host's negotiation policy
must resolve the same version in `resolvedManifest.snapshotSubscriptionVersions`
and include `full` in `resolvedManifest.snapshotModes`. Clients must inspect that resolved
manifest before using this extension. A missing extension version, a missing
`full` mode, compatibility mode, or an uninitialized connection does not
enable subscription requests.

The extension schema is
[`stave.snapshot.subscribe/v1`](../schema/protocol/snapshot-subscriptions.json).
It is separate from [`stave.protocol/v1`](../schema/protocol/protocol.json),
which remains unchanged.

## Subscribe, receive, and unsubscribe

After `stave.initialized`, send an identified request with no parameters:

```json
{"jsonrpc":"2.0","id":3,"method":"stave.snapshot.subscribe","params":{}}
```

A successful response contains a `snapshot` whose `mode` is `full`. This
baseline is written before any subscription notification. Later notifications
use `stave.snapshot.subscription` and contain another full snapshot. This is an
illustrative fragment, not a standalone schema-valid snapshot:

```json
{
  "jsonrpc": "2.0",
  "method": "stave.snapshot.subscription",
  "params": {"snapshot": {"mode": "full", "sequence": 42}}
}
```

Each physical `Server.Serve` JSONL connection permits one subscription. Bind a
session separately for every client connection; do not share one server or
writer among logical subscribers. A second subscribe request on the same
connection is rejected. This keeps each client behind its own writer, so one
slow physical client cannot block another.

The server retains one pending full snapshot per connection. A newer snapshot
replaces that slot, so delivery is a monotonically increasing subsequence of
sequences and never an unbounded queue. Because every delivery is a full
snapshot, coalescing does not require a patch base or a resync request.

Unsubscribe with an identified empty-parameter request:

```json
{"jsonrpc":"2.0","id":4,"method":"stave.snapshot.unsubscribe","params":{}}
```

Unsubscribe is idempotent. Once its acknowledgement is written, no later
notification from that subscription may be written.

## Terminal delivery states and host responsibilities

If delivery stops while the connection can still write, the extension emits
one typed terminal notification:

```json
{
  "jsonrpc": "2.0",
  "method": "stave.snapshot.subscription",
  "params": {"state": "terminated", "reason": "provider_failed"}
}
```

The only terminal reasons are `provider_failed`, `session_closed`, and
`output_limit`. A terminal state removes the subscription. Input EOF,
connection cancellation, server close, or writer failure also ends the
subscription; a writer failure may prevent a terminal message from reaching
that same client.

`Serve` serializes responses and notifications through the connection's single
transport writer. Hosts must provide a writer with bounded, interruptible
writes and close its input or context when the connection ends. A permanently
blocked writer blocks its own physical JSONL stream; subscriptions do not
create a second writer or bypass transport backpressure.

## Authority and compatibility

Subscriptions use the same validated, redacted snapshot path as polling. They
do not include action definitions, invoke actions, change authorization or
confirmation ownership, or grant new authority. Clients that do not offer and
receive `stave.snapshot.subscribe/v1` continue to use `stave.snapshot` polling
with unchanged v1 semantics.

Hosts provide `Options.SubscriptionSnapshotEnvelope` and
`Options.SnapshotPublicationWaiter` to enable the extension. The dedicated
provider must read a full snapshot without advancing the polling provider's
patch history. `BindSession` supplies both callbacks and keeps subscription
reads separate from polling history. A custom host with a stateless provider
may explicitly assign that callback to both options. Without the dedicated
provider, subscription requests are rejected even when the version is offered.
