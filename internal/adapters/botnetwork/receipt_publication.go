package botnetwork

// receiptPublicationSQL runs within one UPDATE over the currently stored row.
// Terminal payloads are immutable; a later source publication can only raise
// their transport Read fact. It does not assert Agent observation. The CTE named
// incoming contains the attempted JSON document and is local to that statement.
const receiptPublicationSQL = `json_set(
 CASE WHEN json_extract(data,'$.state') IN ('reply_stored','failed','refused')
   OR (json_extract(data,'$.state')='unknown'
     AND json_type(data,'$.failure') IS NOT NULL AND json_type(data,'$.failure')!='null')
   OR (json_extract(data,'$.state')='source_executing'
     AND json_extract((SELECT value FROM incoming),'$.state')='accepted')
   OR (json_extract(data,'$.state') IN ('accepted','source_executing')
     AND json_extract((SELECT value FROM incoming),'$.state')='unknown'
     AND (json_type((SELECT value FROM incoming),'$.failure') IS NULL
       OR json_type((SELECT value FROM incoming),'$.failure')='null'))
 THEN data ELSE (SELECT value FROM incoming) END,
 '$.read',json(CASE WHEN COALESCE(json_extract(data,'$.read'),0)
   OR COALESCE(json_extract((SELECT value FROM incoming),'$.read'),0)
 THEN 'true' ELSE 'false' END))`
