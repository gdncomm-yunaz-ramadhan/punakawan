-- Snapshot sources were keyed by the client's agent id on the assumption
-- that a subagent event summarized the subagent's own transcript. It did
-- not: every event summarized whatever transcript path the hook payload
-- carried, and Claude Code hands subagent events the session transcript.
-- So one session stored its whole cumulative usage once per agent id it
-- had seen, plus once under 'main' - and because totals are summed across
-- sources, a delivery reported two or three times the tokens and cost it
-- actually spent.
--
-- Sources are now keyed by the transcript being summarized, so no further
-- duplicates are written. This removes the ones already stored.
--
-- The predicate is exact equality on everything a snapshot counts, which
-- is what makes it safe: two rows that describe the same numbers over the
-- same models are the same reading recorded twice, whereas a genuinely
-- separate subagent transcript differs and survives untouched. 'main' is
-- preferred as the survivor because it is the id the new derivation gives
-- a session's own transcript, so a session still receiving events keeps
-- accumulating onto the row that is kept rather than starting a second.

DELETE FROM agent_usage_snapshots AS dup
 WHERE EXISTS (
   SELECT 1
     FROM agent_usage_snapshots AS keep
    WHERE keep.session_id = dup.session_id
      AND keep.source_id <> dup.source_id
      AND keep.input_tokens = dup.input_tokens
      AND keep.output_tokens = dup.output_tokens
      AND keep.cache_write_tokens = dup.cache_write_tokens
      AND keep.cache_read_tokens = dup.cache_read_tokens
      AND keep.tool_calls = dup.tool_calls
      AND keep.model_usage_json = dup.model_usage_json
      AND (keep.source_id = 'main' OR (dup.source_id <> 'main' AND keep.source_id < dup.source_id))
 );
