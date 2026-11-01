package store

const problemLeaseSchema = `
-- Interaction state is separate from musical evidence and never grants identity.
CREATE TABLE review_problem_lease (
 token TEXT PRIMARY KEY,
 problem_id INTEGER NOT NULL UNIQUE REFERENCES review_problem(id) ON DELETE CASCADE,
 expires_at INTEGER NOT NULL,
 released INTEGER NOT NULL DEFAULT 0 CHECK(released IN (0,1))
);
CREATE TABLE review_problem_operation (
 token TEXT PRIMARY KEY,
 problem_id INTEGER NOT NULL REFERENCES review_problem(id) ON DELETE CASCADE,
 lease_token TEXT REFERENCES review_problem_lease(token) ON DELETE CASCADE,
 scope_hash TEXT NOT NULL DEFAULT '',
 expires_at INTEGER NOT NULL
);
CREATE INDEX idx_problem_operation_scope ON review_problem_operation(problem_id,expires_at);
-- Coalesced work hint only: the consumer must re-read current scope and facts.
CREATE TABLE review_problem_recheck (
 problem_id INTEGER PRIMARY KEY REFERENCES review_problem(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL DEFAULT 1
);
`
