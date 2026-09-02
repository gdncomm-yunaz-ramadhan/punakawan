-- Two Jira sites can issue the same key: PAY-1 at one organisation is not
-- PAY-1 at another. 0034 keyed active-lifetime uniqueness on the issue key
-- alone, which was right while one key could only mean one issue, and
-- becomes wrong the moment a second organisation is configured - the two
-- would collapse into one delivery.
--
-- The organisation is back in the key, but it is no longer free text: it
-- is derived from the site URL at setup time and resolved against the
-- configured organisations before it reaches identity, so the "gdn" versus
-- "gdncomm" split 0034 had to repair cannot recur. A lifetime started
-- before any organisation was resolved keeps its blank tenant and is
-- adopted by the first organisation that names it, rather than duplicated.

DROP INDEX IF EXISTS delivery_cases_active_source_idx;
CREATE UNIQUE INDEX delivery_cases_active_source_idx
ON delivery_cases(source_provider, COALESCE(source_tenant, ''), source_key)
WHERE source_kind = 'jira' AND status = 'active';
