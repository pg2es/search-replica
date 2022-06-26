### Search Replica 
Uses postgres [logical replication protocol](https://www.postgresql.org/docs/current/protocol-logical-replication.html) to subscribe and receive any updates almost in realtime. 

#### Stateless app with no external dependencies

### [Docs](https://pg2es.github.io/) | [DockerHub](https://hub.docker.com/r/pg2es/search-replica)


[![Integration](https://github.com/pg2es/search-replica/actions/workflows/integration.yaml/badge.svg?branch=master)](https://github.com/pg2es/search-replica/actions/workflows/integration.yaml)
[![build and push latest image](https://github.com/pg2es/search-replica/actions/workflows/push.yaml/badge.svg)](https://github.com/pg2es/search-replica/actions/workflows/push.yaml)


Table rows, regardless of types, are encoded into JSON with additional ES metadata, which makes a Message (ES Document).
Those messages are buffered and flushed into ES regularly, on a timeout or when buffer is already large enough.

#### [Check How It Works](how-it-works.md)

Usage E.G:
```bash
go run *.go --help
PG_PUBLICATION="pub1" go run *.go -slot qwerty -reindex
```

#### Roadmap
 - [ ] **Testing** _HELP!_
 - [ ]  ~~Composite arrays~~ _(Too complex; PRs are welcome)_
 - [ ] **Composite PK** / Template fields
 - [ ] Live-reload
 - [ ] Recovery _(Reindex when slot is lost)_
 - [ ] HealthCheck & Readiness
 - [ ] REST API with basic actions
 - [ ] Improve auto discovery, for inlines/join relations
 - [ ] Helm / K8S setup.
 - [ ] "Strict mode"


#### Env Config

| Variable             | Default   | Description |
-----------------------|-----------|-------------|
| PG_SLOT              | pg2es     | replication slot name |
| PG_PUBLICATION       | search    | publication name |
| PGHOST               | localhost | 
| PGPORT               | 5432      | 
| PGDATABASE           | -         |
| PGUSER               | -         |
| PGPASSWORD           | -         |
| SEARCH_HOST          | -         | URL or host of ElasticSearch/OpenSearch
| SEARCH_USERNAME      | -         | optional
| SEARCH_PASSWORD      | -         | optional
| SEARCH_BULK_SIZE     | 4         | (MB) Bulk request size limit.
| SEARCH_PUSH_INTERVAL | 30s       | idle push interval, when there is no enough rows for full bulk request.
| LOG_FORMAT           | json      | json or cli
| LOG_LEVEL            | warn      | from debug to fatal






#### Notes
- The script is **single threaded\*** _(not a bottleneck)_... Separate goroutine is used to make ES requests.
- Links between Database <-> Schema <-> Table <-> Column, shoudld be considered read only, and safe for multithread use... (not yet)
- It's fast. All the the efforts shuld be towards readability, reliability and functionality. 


### Known Limitations:
- Inlines can not update PK/ID/Routing fields (yet)
- No 1:1 inlines (yet)
- Delete document deletes all inlines (AKA DELETE CASCADE), and they can not be restored.
