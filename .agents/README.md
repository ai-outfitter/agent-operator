# Link Operator Dotagents catalog

This directory is a standalone Dotagents payload used by Link Operator agents.
The M1 `researcher` agent and both skills are vendored so one catalog is
sufficient and M1 does not depend on cross-catalog override behavior.

## Vendored resources

| Resource | Upstream | Revision |
| --- | --- | --- |
| `skills/wiki` | `ncrmro/.agents` | `0750c51f7afc236d85ed43fe6f032a1ffa6be88b` |
| `skills/source-ingest` | `scifireality/artera/.agents` | `a621fe191bb1e758839fd99322e4e134d02698e9` |

The vendored files are copied byte-for-byte from those revisions. Changes
should be made upstream first and then synchronized here with an explicit
revision update and diff review.

## Contents

```text
.agents/
├── settings.yml
├── agents/
│   └── researcher/
│       └── agent.md
└── skills/
    ├── source-ingest/
    └── wiki/
```
