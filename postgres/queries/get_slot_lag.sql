-- Query for periodical check of WAL lag. 
-- Returns LSN positions and size of WAL, blocked from GC by this slot.
-- $1 is a slot name
SELECT 
    pg_current_wal_lsn()::TEXT AS current_lsn, 
    confirmed_flush_lsn::TEXT AS flushed_lsn, 
    pg_size_pretty(pg_current_wal_lsn() - confirmed_flush_lsn) AS size,
    pg_replication_slots.wal_status -- if 'lost', we need reindex everything
FROM pg_replication_slots
WHERE slot_name=$1;
