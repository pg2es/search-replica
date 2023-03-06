### Search Replica 

Replicates PostgreSQL `primary` database into Elasticsearch/OpenSearch index `read-only replica`.

Does not rely on additional database queries or external queues, 
using exclusively [logical replication protocol](https://www.postgresql.org/docs/current/protocol-logical-replication.html), 
allowing almost realtime consistent synchronisation without external dependencies. 

Focused on speed and efficiency. 

### [Docs](https://pg2es.github.io/) | [DockerHub](https://hub.docker.com/r/pg2es/search-replica) | [Try it](https://pg2es.github.io/getting-started/ "in 5min locally with docker-compose demo")

[![Integration](https://github.com/pg2es/search-replica/actions/workflows/integration.yaml/badge.svg?branch=master)](https://github.com/pg2es/search-replica/actions/workflows/integration.yaml)
[![build and push latest image](https://github.com/pg2es/search-replica/actions/workflows/push.yaml/badge.svg)](https://github.com/pg2es/search-replica/actions/workflows/push.yaml)

- **Consistent and fault tolerant, without dependencies**  
  Thanks to PostgreSQL replication slots mechanisms. 
- **Initial (re)indexing**  
  Using `COPY` command
- **Uses native PG protocol**  
  Both Text and Binary form.
- **Full DB types support**  
  json fields, composite types, arrays, enums... Except of arrays of composite types. 
- Native Parent/Child join
  Including document `_id` and `routing` control
- Limited denormalization & document modifications.
  check [inlining](https://pg2es.github.io/getting-started/inline/)
- Bulk requests
  Data is flushed to Elasticsearch/OpenSearch in bulk.


---

### Configuration

#### ConfTags

You can 
- set `routing` and document `_id` fields;
- rename or skip fields;
- define parent/child `join` field;
- inline rows as object into parent document;
- set custom inlining script;
- ~~set templated fields~~ _[(planned)](https://github.com/pg2es/search-replica/issues/5)_
- ~~json-path names~~ _(planned)_ 

Using `COMMENT`s in your database schema. Check
[syntax and description](https://pg2es.github.io/getting-started/conftags/ "conftags and syntax")


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
| SEARCH_PUSH_THROTTLE | 500ms     | hard limit. At most one request during this period.
| SEARCH_PUSH_DEBOUNCE | 500ms     | delays bulk after idle, to fetch related data.
| LOG_FORMAT           | json      | json or cli
| LOG_LEVEL            | warn      | from debug to fatal



#### Notes
- The script is **single threaded\*** _(not a bottleneck)_... Separate goroutine is used to make ES requests.
- Links between Database <-> Schema <-> Table <-> Column, shoudld be considered read only, and safe for multithread use... (not yet)
- It's fast. All the the efforts shuld be towards readability, reliability and functionality. 

#### Known Limitations:
- No 1:1 inlines (yet)
- Delete document deletes all inlines (AKA DELETE CASCADE), and they can not be restored.
