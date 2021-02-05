# Go Feed

Go Feed is a queue scheduling system, built on top of a relational database. Taking inspirations from Cosmos DB's [Change Feed Processor](https://docs.microsoft.com/en-us/azure/cosmos-db/change-feed-processor), Go Feed provides the benefits of a queryable, persistant storage system on top of a Queue. It improves upon the Change Feed by adding built-in error handling and retry mechanisms, a flexible interface for processing work, and [Checkpointing](#checkpointing)

The state processor is a distributed, sharded (work-stealing) change feed that constantly polls the database for
available work.

We define work that needs to fan out/fan in, as any work where a single item triggers multiple sub items, and where
those sub items might need to reach a specific state before continuing subsequent steps. Imagine a scenario where you
are processing applications for a job resume, and you want to pick the top 10 candidates. You may send each resume
to an OCR service for text exraction and classification. When a single resume is classified, you can't be sure where it
fits in the ranking, so instead you insert a checkpoint to wait for all resumes to be classified, then move on to
ranking.

This fan out/fan in is accomplished through the use of `Partitions` and `Items`.

## Quick Start

To see the set of arguments, and a description of each:

`DOCKER_BUILDKIT=1 docker run -it $(docker build -q -f Processor.Dockerfile .) --help`

The code is located in the root folders `internal` and `cmds`.

### Supported Databases

The processor is tested with SQL Server and SQLite3, although should work with any DB that Gorm supports.

## Items

Processor Items represent an item of work, and belongs to a single partition. An item has some basic metadata to help
with ordering, and keeping track of which items are ready to be processed, or if there were any failures in processing.
However the bulk of the data is stored as a serialized set of `bytes`, which get forwarded to a `Processor` interface.
As of now, we have defined a single interface, the HTTProcessor, which forwards an item's bytes to a service over HTTP.

The Processor interface is very small, so it would be trivial to build a processor that implements batching, gRPC, or
uses the watcher as a library to contain processing to a single binary.

### Lease Status

A `status` field is present on each state, and represents the current status. The 3 possible values are:

* Available - potentially ready to be processed
* Failed - failed processing
* Complete - completed.

### Processing

The State Processor is built around the [watcher](../../internal/state/watcher.go), which contains the bulk of the logic
for querying the database, leasing partitions, and forwarding state objects to be processed. We leverage dependency
injection to allow the caller to implement whichever Processor they choose. The current implementation is here The
current Processor implementation is [here](../../internal/processor/processor.go), and simply marshals the data to JSON
and forwards to the Handler.

Since the Processor doesn't know about the data, it doesn't know about the Handler's concepts of the internal state
machine, ie: "ready", "processed", "text_validated". As such, it doesn't know when a state reaches a terminal point,
to mark that State as "done". To accomplish this, the interface for the processor is:

```go
type Response struct {
    Gate    int                    `json:"gate"`
    Done    bool                   `json:"done"`
    DataMap map[string]interface{} `json:"data"` // This field is simply a helper for marshaling contents.
    Data    string                 `json:"-"`
}
```

The handler itself returns the above message, controlling the flow of state processing by indicating the next gate
(gates are described below under Partition fan/out fan/in), or if we have reached a terminal state via the `done` field.

## Processor Partitions

A partition maps to a top level work item, ie: a work item that may need to "fanout", like a folder, and leverages a
has-many relationship to states. Partitions are ready for processing when the status is set to `Available`.
The state processors are continuously polling the database for available partitions that are "unleased".

### Leasing a Partition

Once an unleased partition is found, a processor will set it's Owner field to a UUID unique to that instance of the
processor, and the Until time to some period in the future. Using [OCC](#optimistic-concurrency-control) we save the
partition, which grants "ownership" to this instance of the state partition, which will then begin polling for states.

Since processor's are constantly trying to lease partitions, multiple processor's may attempt to lease the same
partition, or even "steal" a partition from another.

### Checkpointing

Partitions enable checkpointing by introducing the concept of a `gate`. The main query polling for states
queries based on the current partitions `gate` field. If no states are found, we trigger a method that checks if we
should close a partition, by checking the count of states grouped by `status`, and trigger the checks mentioned above
for closing out a partition. If states are found in "available", but none in failed, this means we can increment the
partition's gate, and begin processing the next set of states.

### Caveats

There are a few caveats to consider when using the State Processor.

1. When setting `AutoClose` to false, the State Processor will attempt to close a partition (mark as Complete), if all
   states are Complete. This means, if not writing to the database by the provided libraries, you should write a
   partition *after* you write *all* of the items for that partition.
  * Alternatively, you can later update the partition to make sure it is in the `Available` state.

1. You cannot add an item at a lower checkpoint than the partition is currently at, as it will never get processed, and
   additionally prevent marking the partition as complete.
  * Alternatively, you can set `ManualCheckpoint` to true on the watcher, which will prevent automatically

incrementing checkpoints.

WARNING: Since AutoClose defaults to false, if you are not closing out your partitions, you need to be careful of memory
pressure, since we don't limit the number of results from GetAvailablePartitions. This can also be alleviated by creating
more watchers, since they will steal each others leases, and them from showing up in results for other processors.

## Optimistic Concurrency Control

All data saved by the processor leverages Optimistic Conccurency Controll (OCC) to protect against other workers
leasing, or processing the same data twice. This is done with the `version` field present on both states and partitions,
 and takes place of the form:

`UPDATE <table> SET version=$current_version+1 <other fields ...> WHERE id = $id AND version=$current_version`

This checks that the version matches on the update, and protects simultaneous writes.

## Other items

Currently, schema migrations are done automatically, using the internal ORM. Future, more complicated schema migrations
may be required. To disable this you can remove any calls to `AutoMigrate` in the code base.
