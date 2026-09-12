# sac

`sac` is a small Go CLI/library for content-addressed storage plus a local SQLite inventory of known blobs.

It can:

- store files by SHA-256 digest in a sharded content-addressed store;
- deduplicate repeated writes;
- record each successful write by digest and size in SQLite;
- verify every cataloged blob against its on-disk SHA-256 digest.

## Status

This project is **AI-generated and under construction**. It is dead simple by design. LLMs are strong at docstring-to-function type coding where the problem is narrowly specified and clearly defined.

Treat the API, CLI flags, storage layout, and schema as unstable until the project has real releases and production hardening.

## Quick example

```bash
# Store a file, catalog it, and record an observed source URI
sac --store ./store --catalog ./catalog.sqlite put --locator file:///imports/data.csv ./data.csv

# Retrieve a blob by digest
sac --store ./store get <sha256-hex> > data.csv

# Verify cataloged blobs against the store
sac --store ./store --catalog ./catalog.sqlite verify

# Find cataloged artifacts by locator prefix
sac --catalog ./catalog.sqlite locate --prefix file:///imports/
```

## Packages

- `cas` — content-addressed blob storage.
- `catalog` — SQLite inventory of known blobs.
- `cmd` — Cobra CLI commands.

## Intended use

`sac` is deliberately generic. Downstream projects can use it as a preservation and provenance primitive without embedding domain-specific assumptions.

## License

MIT. See [`LICENSE`](LICENSE).
