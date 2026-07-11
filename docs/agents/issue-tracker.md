# Issue Tracker

This repository uses a local Markdown issue tracker.

## Wayfinding operations

- Issue files live in `docs/agents/issues/`.
- New wayfinding efforts should use one topic folder per issue map, for example `docs/agents/issues/reliable-connection-and-reconnect-experience/`.
- Inside a topic folder, issue files keep local numeric prefixes starting from `0001`; do not continue the global numeric prefixes from older topic files.
- The map issue is labelled with `wayfinder:map`.
- Child issues point back to their map with `Parent: <map file>`.
- Blocking is expressed with `Blocked by: <issue file>`. If empty, the issue is unblocked.
- Claiming is expressed with `Assignee: <name>`. Empty means unclaimed.
- Closing is expressed with `Status: closed`; open work uses `Status: open`.
- Resolution comments are appended under `## Resolution` before closing a ticket.

The frontier is the set of open child issues whose `Assignee` is empty and whose `Blocked by` entries are all closed.
