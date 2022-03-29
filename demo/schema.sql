DROP PUBLICATION IF EXISTS "search";
DROP TABLE IF EXISTS "main_doc";
DROP TABLE IF EXISTS "inline_doc";
DROP TABLE IF EXISTS "child_doc";


CREATE TABLE "main_doc" (
    id text NOT NULL,
    -- docType will be injected by SearchReplica
    "date" timestamp with time zone NOT NULL DEFAULT now(),
    deleted boolean NOT NULL DEFAULT false,
    -- inlined field will be updated in ElasticSearch
    -- Pure Elastic feature. No effort here
    nested jsonb NOT NULL DEFAULT '[]'::jsonb,
    non_searchable_field text NOT NULL DEFAULT ''::text,
    text text NOT NULL DEFAULT ''::text,
    text_array text[] NOT NULL DEFAULT ARRAY[]::text[],
    ignore_me text NOT NULL DEFAULT 'dont search me'::text,

    CONSTRAINT main_doc_pkey PRIMARY KEY (id)
);

/* Inlines implemented as scripted updates, increase load during indexing.Suitable fow low cardinality 1:M data. */
CREATE TABLE "inline_doc" (
    id text NOT NULL,
    parent_id text NOT NULL,
    -- docType will be injected by SearchReplica
    "value" text NOT NULL DEFAULT 'hello world'::text,
    ignore_me text NOT NULL DEFAULT 'dont search'::text,

    CONSTRAINT inline_doc_pkey PRIMARY KEY (id)
);

/* Joins are native ElasticSearch feature. Rows are indexed as regular documents. Have search-time penalty, due to complex queries and global ordinals. */
CREATE TABLE "child_doc" (
    id text NOT NULL,
    parent_id text NOT NULL,
    "value" text NOT NULL DEFAULT 'hello world'::text,
    ignore_me text NOT NULL DEFAULT 'dont search me'::text,

    CONSTRAINT child_doc_pkey PRIMARY KEY (id)
);


/* To make inline delition possible REPLICA IDENTITY needs to be properly configured, to include `parent`, `pk`, and `routing` fields. Otherwise those fields are omited from WAL.
See: https://www.postgresql.org/docs/10/sql-altertable.html#SQL-CREATETABLE-REPLICA-IDENTITY
*/

/* complex example with unique index which includes all required fields */
CREATE UNIQUE INDEX "inline_uniq_index" ON "inline_doc" (id, parent_id);
ALTER TABLE "inline_doc" REPLICA IDENTITY USING INDEX inline_uniq_index;
/* or simpler index, less efficient, where all old fields  would be written to WAL */
ALTER TABLE "child_doc" REPLICA IDENTITY FULL;


--
-- CONFIGURATION
--

COMMENT ON TABLE "main_doc" IS 'index:"main,all" join:"join,immaparent" inline:"inline_name,inlined_field"';
COMMENT ON COLUMN "main_doc".id IS 'index:",routing,id"';
COMMENT ON COLUMN "main_doc".ignore_me IS 'index:"-"';

/* Just inline and do not index as separate document.
We need to specify inline PK, parent document ID and optionally routing.
Each inlined field should be explicitly defined as such. */
COMMENT ON TABLE "inline_doc" IS 'index:"-"';
COMMENT ON COLUMN "inline_doc".parent_id  IS 'inline:"inline_name,_pk,parent,routing"';
COMMENT ON COLUMN "inline_doc".id  IS 'inline:"inline_name,pk"';
COMMENT ON COLUMN "inline_doc".value  IS 'inline:"inline_name"';


/* Be aware, child document should always be in the same shard as parent, thus routing field is used. It can be any shard key, or just a parent ID like shown below */
COMMENT ON TABLE "child_doc" IS 'index:"child,all" join:"join,immachild"';
COMMENT ON COLUMN "child_doc".id IS 'index:",id"';
COMMENT ON COLUMN "child_doc".parent_id IS 'index:",routing" join:"parent"';



-- Any table needs to be exposed via PUBLICATION
CREATE PUBLICATION "search"
    FOR ALL TABLES -- Optionally you can specify target tables
    WITH (
	-- Events we care about. Truncate can be dangerous
	publish = 'insert, update, delete, truncate', 
	-- Publish multiple partitions as a single table
	publish_via_partition_root = true  
);
