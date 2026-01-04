# Indexing basics

For some resource (ex: a repository), the indexer should periodically re-index
it. The indexing worker does so by issuing a query like,

```sql
UPDATE some_resource
SET indexing_began = NOW()
WHERE id = (
    SELECT id
    FROM some_resource
    WHERE indexing_began + (<re-index TTL>) < NOW()
    AND indexing_finished + (<re-index period>) < NOW()
    ORDER BY indexing_finished ASC
    LIMIT 1
)
RETURNING id;
```

Breaking this down:


- The inner query `ORDER...LIMIT` returns the row that was indexed the longest
ago.
- The inner query `AND indexing_finished [...]` filter excludes any rows indexed
within an acceptable amount of time ago (the re-index period).
- The inner query `WHERE indexing_began [...]` excludes rows that are busy
indexing (their time is within the re-indexing TTL, set by some other worker).
- The outer query sets indexing_began to `NOW`, "claiming" the row. It has until
the re-index TTL until another worker can claim the same row.
- The outer query returns the id of the row it claimed, so that the code can
begin work indexing the resource.
- This query will lock the entire table. Workers will call this query to accept
new work. When nothing is returned by this query, it means there is no work to
do, and works should wait before trying again.