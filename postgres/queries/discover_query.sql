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
		WHEN t.relreplident = 'd' THEN COALESCE(i.indisprimary, false) -- default: primary key if any
		WHEN t.relreplident = 'i' THEN COALESCE(i.indisreplident, false) -- indice: columns of uniq index
		WHEN t.relreplident = 'f' THEN true -- full: all columns
	END saved_in_wal
	--  a.attrelid as relation_oid -- table type (check if same as in streaming protocol)
FROM pg_publication_tables AS pt
	INNER JOIN pg_namespace s ON  pt.schemaname = s.nspname
	INNER JOIN pg_class t ON  pt.tablename = t.relname AND t.relnamespace = s.oid
	INNER JOIN pg_attribute a ON a.attrelid = t.oid
	LEFT JOIN  (
		SELECT -- group indice info by column before join
			i.indrelid,
			-- Only key columns are saved in WAL, so we filter out rest.
			-- i.indkey is array of columns with size i.indatts
			-- which starts with key-columns up to t.indkeyatts.
			-- 'i.indnkeyatts-1' used because arrays start from zero.
			unnest(i.indkey[:i.indnkeyatts-1]) AS attnum,
			bool_or(i.indisprimary) as indisprimary,
			bool_or(i.indisreplident) as indisreplident
		FROM pg_index as i
		WHERE i.indisprimary OR i.indisreplident -- We are interested in PK and WAL fields only
		GROUP BY i.indrelid, attnum
	) i ON a.attnum = i.attnum AND a.attrelid = i.indrelid
WHERE pt.pubname=$1
  -- negative numbers are reserved for system columns.
  AND a.attnum > 0 
  AND NOT a.attisdropped 
  -- only [r]ealations and [p]artitions. [m]aterialized_views and other types are not supported by PG.
  AND t.relkind IN ('r', 'p')
  -- skip system tables
  AND s.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast') 
ORDER BY s.nspname, t.relname, a.attnum;
