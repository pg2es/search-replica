-- This query returns all array types for user defined enum types
SELECT 
	pg_type.oid AS array_typ,
	pg_type.typelem AS enum_typ
FROM pg_type 
WHERE
	typcategory = 'A' AND
	pg_type.typelem IN (
		SELECT DISTINCT pg_enum.enumtypid
		FROM pg_enum
	)
