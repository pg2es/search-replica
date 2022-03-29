-- Query for discovering tables to be indexed by OpenSearch.
-- $1 is the name of the publication.
SELECT 
	s.nspname, -- namespace/schema
	t.relname, -- table 
	a.attname, -- column
	obj_description(concat(s.nspname, '.', t.relname)::regclass::oid) as table_comment,
	col_description(concat(s.nspname, '.', t.relname)::regclass::oid, a.attnum) as column_comment,
	COALESCE(i.indisprimary, false) as pk, -- is column part of PK
	a.atttypid as typ_oid,
	CASE -- check which columns are stored in WAL for update/delete operations
		WHEN t.relreplident = 'n' THEN false -- nothing: none
		WHEN t.relreplident = 'd' THEN COALESCE(i.indisprimary, false) -- default: rimary key if any
		WHEN t.relreplident = 'i' THEN COALESCE(i.indisreplident, false) -- indice: columns of uniq index
		WHEN t.relreplident = 'f' THEN true -- full: all columns
	END saved_in_wal
	--  a.attrelid as relation_oid -- table type (check if same as in streaming protocol)

FROM pg_publication_tables AS pt
	INNER JOIN pg_namespace s ON  pt.schemaname = s.nspname
	INNER JOIN pg_class t ON  pt.tablename = t.relname AND t.relnamespace = s.oid
	INNER JOIN pg_attribute a ON a.attrelid = t.oid
	LEFT JOIN  pg_index i ON a.attnum = ANY(i.indkey) AND a.attrelid = i.indrelid

WHERE pt.pubname=$1
  AND a.attnum > 0 -- negative numbers are reserved for system columns.
  AND NOT a.attisdropped 
  AND t.relkind IN ('r', 'p') -- only [r]ealations and [p]artitions. [m]aterialized_views and other types are not supported by PG. 
  AND s.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast') -- skip system tables
  ORDER BY s.nspname, t.relname, a.attnum;
