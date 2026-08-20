-- Turn AI copywriting back on for every account.
--
-- Accounts created through the web UI got use_ai = false, because the create
-- handler never set the field and Go's zero value for a bool is false. Those
-- accounts rendered the raw scraped Amazon title and bullet list straight into
-- their template, which is why their posts read as a product spec dump instead
-- of the styled copy the templates were written for.
--
-- Run:  psql -h 127.0.0.1 -U postgres -d postgen -f enable_ai_for_accounts.sql

BEGIN;

SELECT name, use_ai AS before FROM accounts ORDER BY name;

UPDATE accounts SET use_ai = true WHERE use_ai = false;

SELECT name, use_ai AS after FROM accounts ORDER BY name;

COMMIT;
