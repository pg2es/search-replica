### Search Replica
Uses postgres [logical replication protocol](https://www.postgresql.org/docs/current/protocol-logical-replication.html) to subscribe and receive any updates almost in realtime. 

Table rows, regardless of types, are encoded into JSON with additional ES metadata, which makes a Message (ES Document).
Those messages are buffered and flushed into ES regularly, on a timeout or when buffer is already large enough.

(someone on a stackoverflow said, that recommended bulk size is just a 5mb), with limit ~100MB. It seems, that increasing size of a bulk query does not improve throughput. And 4MB works better than 64MB, at least for local workloads.

#### [Check How It Works](how-it-works.md)



Usage E.G:
```bash
go run main.go --help

go run main.go -slot qwerty -publication pub1 -reindex
go run main.go -slot qwerty -publication pub1 -recreate
```



How to configure DB?

A bit hacky, yet easy to use approach was selected: PostgreSQL [COMMENTs](https://www.postgresql.org/docs/current/sql-comment.html).
So, in order to make table `users` indexable. If at leas one field specified as indexable, table would be indexed. 

Comment configuration syntax is Go structtags with exception, that one tag can be set multiple times. Strings are concatenation of optionally space separated `key:"value,value2"` pairs  

E.G: `tag1:"val1" tag2:"val2,opt1,opt2"tag2:",opt1,opt2"`


```sql
-- Theese are subject to improve/change
COMMENT ON TABLE users IS 'index:",all"' -- to index all it's field by default
COMMENT ON COLUMN users.password_hash IS 'index:"-"' -- to skip this field
COMMENT ON COLUMN users.first_name IS 'index:"name"' -- to index and rename the field
```

Also, all the tables should be included in Publication,
```sql
-- for all tables in database
CREATE PUBLICATION "ExamplePublication" FOR ALL TABLES
WITH (publish = 'insert, update, delete, truncate');

-- or you can specify tables
CREATE PUBLICATION "ExamplePublication" FOR TABLE public.users, schemaname.tablename, etc.etc
WITH (publish = 'insert, update, delete, truncate');
```

Important to specify proper `REPLICA IDENTITY` for each table (more examples in ./demo/schema.sql). Unless Elasticsearch and Postgres PK is equal and optional routing field is part of PK or not defined.
```
ALTER TABLE "table_name" REPLICA IDENTITY FULL;
```


All tables included in publication would be indexed by default. However you can overwrite this behavior.


Special `docType` _(in future configurable)_ field is attached to each document. 

#### Roadmap
 - [x] **Arrays and Custom types (E.G. Enums support)**
   - [x]  Basic Arrays _(like `Text[]`)_
   - [x]  Enums
   - [x]  Enum arrays
   - [x]  Composite
   - [x]  ~~Composite arrays~~ (Too complex; PRs are welcome)
 - [ ] **Composite PK**
 - [ ] **Better logging**
 - [ ] Reindex when slot is lost
 - [ ] HealthCheck & Readiness
 - [ ] Metrics _(prometheus)_
 - [ ] REST API with basic actions
 - [ ] Tests
 - [ ] Improve auto discovery, for inlines/join relations




#### How it works

##### 1. Descovery & initial config
There is sql query, to fetch all tables, columns and their comments for all tables within Publication. 
With this information it's possible to build config, with tables, columns, schemas, parsing awailable table comments.

###### 1.1 Slot is (re) created

##### 2. Cold start (optional)
Having tables and columns _(only names)_ that needs to be indexed, we can make a `SELECT * FROM ...` query, and use it's result, to determine column type.
There is `switch/case` mapping for all default PG type. So we can decode values.

Also, during this step, column names got their json representation "pre-marshaled". 
Having thet we can iterate over result, decoding each row and generating JSON for ES. Marhaling only values. (most of them are `json.Marshaller`s).
Those chunks of json are send to chanel.

##### 3. Subscribtion
_After cold start, we have current PostgreSQL log position, so we can subscribe only for new events, even if slot is lagging behind._

Use logical replication subscribtion without specifying a position, will use last commited position as stating point. So, in case of crashes, network outage, or overloaded search we are safe. Script always start from a last checkpoint.


#### Inlining (1:N sub documents)

This is done using scripted update of parent document. Does not require any effort from Postgres, while slightly increasing a load on elasticsearch.

**WARNING:** N:M relations are not supported. And 1:1 do not make any sense in this context.


In order to put one table as list of objects into another doc, you need to have `parent key`, which is usualy foreign key.
Imagine we have structire like thise.

So, having two tables with 1-N relationship, like
<table>
<tr><th>Parent</th><th>Child</th></tr>
<tr><td>
 
| Users   | `index:",all" inline:"comments_inl,comments"`
| --------| --------------------------------------------
| id (pk) |
| name    |
| email   |
| phone   | `index:"-"`

</td><td>
 
| Comments | `index:"-"`
| -------- | -------------------------------
| id (pk)  | `inline:"comments_inl"`
| user_id  | `inline:"comments_inl,parent"`
| text     | `inline:"comments_inl,content"`
| date     |


</td></tr> </table>
 
here:
- `index:",all"` comment on Users table means index all fields, with `documentType={tablename}`
- `inline:"comments_inl,comments"` on Users table would inline `comments_inl` objects into `comments` field.
- `index:"-"` on a Users.phone would not index this field, regardless `index:",all"` on a table.
- `index:"-"` on Comments table, would not index this table entierly.
- `inline:"comments_inl"` on Comments field id would include this field in `comments_inl` inline, preserving field name. And since this column is PK, it would also be used as PK for inline.
- `inline:"comments_inl,parent"` on `user_id` would specify PK of parrent document. So we can append comment into User.comments.
- `inline:"comments_inl,content"` on `text` field id would include this field in `comments_inl` inline under new `content` name.

and this would produce documents like 
```json
"_source": {
    "docType": "users",
    "id": 1631208,
    "name": "Rick Sanchez",
    "email": "rick@sanches.co",
    "comments": [{
        "id": 25285149,
        "content": "Lorem Ipsum ...",
        "user_id": 1631208
    },{
        "id": 25285753,
        "content": "... dolore eu fugiat nulla pariatur...",
        "user_id": 1631208
    }]
}

```

#### Possible configuration tags:

#### Table
- `index`:
  - docType  _(`-` means do not index)_
  - Opts:
    - `all` (deprecated) index all fields in this table
- `inline`:
  - inline name, that will be injected as a field
  - field name
  - _(optional)_ script name for adding
  - _(optional)_ script name for removal
- `join`:
  - field name
  - _(optional)_ type name _(by default docType is used)_

#### Field
- `index`:
  - rename  _(`-` means do not index)_
  - Opts:
    - `pk` this value, prefixed by table name would be used as document _id.
    - `id` value would be used as document [_id field](https://www.elastic.co/guide/en/elasticsearch/reference/7.10/mapping-id-field.html) 
    - `routing` value would be used for [routing](https://www.elastic.co/guide/en/elasticsearch/reference/7.10/mapping-routing-field.html)
- `inline`:
  - inline name, where this field should be used
  - rename (within inline) NOT IMPLEMENTED
  - Opts:
    - `pk` inline PK
    - `parent` parent `_id`
    - (optional) `routing` routing value of PARENT document. (in order to find and update it)
- `join`:
  - Opts:
    - `parent` field used as parent value
    - `name` field is used as join type (for polymorphism)




#### Notes
- The script is **single threaded** _(not a bottleneck)_... Separate goroutine is used to make ES requests and for periodical logs.
- Links between Database <-> Schema <-> Table <-> Column, shoudld be considered read only, and safe for multithread use... (not yet)
- It's fast. All the the efforts shuld be towards readability, reliability and functionality. 


### Known Limitations:
- PK/ID/Routing field can not be updated. (yet; undefined behavior)
- Inline can not change parent
- No composite PKs (yet)
- No 1:1 inlines (yet)
- Delete document deletes all inlines (AKA DELETE CASCADE), and they can not be restored.



#### Inlining
![image](https://user-images.githubusercontent.com/9192931/127616210-51b0fd08-81b1-4fdd-90c9-d5ac48119fba.png)
