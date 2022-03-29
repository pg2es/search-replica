SELECT 
	pg_enum.oid AS value_oid,
	pg_enum.enumtypid AS enum_oid, 
	pg_enum.enumlabel AS value_text, 
	pg_type.typname AS enum_name, 
	pg_namespace.nspname AS namespace
FROM pg_catalog.pg_enum
JOIN pg_catalog.pg_type ON pg_type.oid = pg_enum.enumtypid 
JOIN pg_catalog.pg_namespace ON pg_namespace.oid = pg_type.typnamespace
;

