# sac

`sac` is a small Go CLI/library for content-addressed storage plus a local, hash-chained lineage log.

It can:

- store files by SHA-256 digest in a sharded content-addressed store;
- deduplicate repeated writes;
- record ingest/transform-style events in SQLite;
- link event inputs and outputs by artifact hash;
- verify the event log's hash chain.

## Status

This project is **AI-generated and under construction**. Treat the API, CLI flags, storage layout, and schema as unstable until the project has real releases and production hardening.

## Quick example

```bash
# Store a file and record a lineage event
sac --store ./store --log ./lineage.sqlite put ./data.csv

# Retrieve a blob by digest
sac --store ./store --log ./lineage.sqlite get <sha256-hex> > data.csv

# Verify the lineage log hash chain
sac --log ./lineage.sqlite verify
```

## Packages

- `cas` — content-addressed blob storage.
- `lineage` — SQLite-backed hash-chained event log.
- `cmd` — Cobra CLI commands.

## Intended use

`sac` is deliberately generic. Downstream projects can use it as a preservation and provenance primitive without embedding domain-specific assumptions.

## License

MIT. See [`LICENSE`](LICENSE).
