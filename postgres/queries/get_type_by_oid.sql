-- Selects basic type description for specific oid
SELECT 
	t.oid, 
	typname, 
	typtype, -- [b]ase, [e]num, [c]omposite, [p]seudo, [r]ange
	typcategory, -- [E]num, [A]rray, [C]omposite
	typarray, -- register array OID at the same time
	typelem, -- oid of element type (for arrays)
	typrelid -- OID of "relation" which describes composite type
FROM pg_namespace ns
JOIN pg_type t ON t.typnamespace = ns.oid

WHERE 
  ns.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast') 
  AND t.oid = $1
