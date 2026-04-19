-- Rename phase identifiers to meaningful names and remove obsolete phases.
-- phase1 → missing_info, phase2 → doc_search, phase4a → enhancement_context
-- phase3 and phase4b are removed (lean bot pivot, PR #60).

UPDATE bot_comments SET phases_run = array_replace(phases_run, 'phase1', 'missing_info');
UPDATE bot_comments SET phases_run = array_replace(phases_run, 'phase2', 'doc_search');
UPDATE bot_comments SET phases_run = array_replace(phases_run, 'phase4a', 'enhancement_context');
UPDATE bot_comments SET phases_run = array_remove(phases_run, 'phase3');
UPDATE bot_comments SET phases_run = array_remove(phases_run, 'phase4b');

-- triage_results was dropped in migration 008 but the column rename may
-- have been run before 008 on some historical DBs; skip if the table is gone.
DO $$
BEGIN
    IF to_regclass('public.triage_results') IS NOT NULL THEN
        UPDATE triage_results SET phases_run = array_replace(phases_run, 'phase1', 'missing_info');
        UPDATE triage_results SET phases_run = array_replace(phases_run, 'phase2', 'doc_search');
        UPDATE triage_results SET phases_run = array_replace(phases_run, 'phase4a', 'enhancement_context');
        UPDATE triage_results SET phases_run = array_remove(phases_run, 'phase3');
        UPDATE triage_results SET phases_run = array_remove(phases_run, 'phase4b');
    END IF;
END $$;

UPDATE triage_sessions SET phases_run = array_replace(phases_run, 'phase1', 'missing_info');
UPDATE triage_sessions SET phases_run = array_replace(phases_run, 'phase2', 'doc_search');
UPDATE triage_sessions SET phases_run = array_replace(phases_run, 'phase4a', 'enhancement_context');
UPDATE triage_sessions SET phases_run = array_remove(phases_run, 'phase3');
UPDATE triage_sessions SET phases_run = array_remove(phases_run, 'phase4b');
