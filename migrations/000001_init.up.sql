CREATE TABLE teams_reindexing (
    -- Only the value "true" should be present. Limits the table to one row.
    id BOOL PRIMARY KEY DEFAULT TRUE,

    -- Workers should re-index the list of all GitHub teams when:
    --     NOW > indexing_finished + re-index-period, and
    --     NOW > indexing_began + indexing-ttl
    indexing_began TIMESTAMP,
    indexing_finished TIMESTAMP
);

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(200) UNIQUE NOT NULL
);

CREATE TABLE teams (
    id SERIAL PRIMARY KEY,
    name VARCHAR(200) UNIQUE NOT NULL
);

-- Many-to-many.
CREATE TABLE team_members (
    team_id INT NOT NULL,
    user_id INT NOT NULL,
    PRIMARY KEY (team_id, user_id),
    FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

-- Only used for re-indexing queue.
CREATE TABLE repo_indexing (
    -- Only the value "true" should be present. Limits the table to one row.
    id BOOL PRIMARY KEY DEFAULT TRUE,

    -- Workers should re-index the list of all repos when:
    --     NOW > indexing_finished + re-index-period, and
    --     NOW > indexing_began + indexing-ttl
    indexing_began TIMESTAMP,
    indexing_finished TIMESTAMP
);

-- Populate the initial value.
INSERT INTO repo_indexing (id, indexing_began, indexing_finished)
VALUES (TRUE, TIMESTAMP '-infinity', TIMESTAMP '-infinity')
ON CONFLICT (id) DO NOTHING;

-- A listing of all repos, and when to work on them next.
-- Both a re-indexing queue and a read table.
CREATE TABLE repos (
    -- A more efficient surrogate key for org_repo_name.
    repo_id BIGSERIAL PRIMARY KEY,

    -- Something like "corp/my-repo".
    org_repo_name VARCHAR(200) UNIQUE,

    -- Something like "main".
    default_branch_name VARCHAR(200) NOT NULL,
    
    -- Workers should re-index a repo (both PRs and commits) when:
    --     NOW > indexing_finished + re-index-period, and
    --     NOW > indexing_began + indexing-ttl
    indexing_began TIMESTAMP DEFAULT TIMESTAMP '-infinity',
    indexing_finished TIMESTAMP DEFAULT TIMESTAMP '-infinity'
);

-- A listing of all the PRs for each repo.
CREATE TABLE repo_prs (
    repo_id BIGINT NOT NULL REFERENCES repos(repo_id) ON DELETE CASCADE,
    pr_number INT,
    created TIMESTAMP,
    merged TIMESTAMP,
    PRIMARY KEY(repo_id, pr_number)
);

-- A listing of the reviewers for each PR.
-- TODO(jbarkhuysen): Add time-based metrics later (time to first review, etc).
CREATE TABLE pr_reviewers (
    repo_id BIGINT NOT NULL,
    pr_number INT NOT NULL,

    -- Who reviewed the PR: someone that either commented on the PR or approved
    -- it.
    reviewer_email VARCHAR(255) NOT NULL,

    -- How many times they commented on the PR.
    num_comments INT NOT NULL,

    -- Whether they approved the PR.
    approved BOOL NOT NULL,

    PRIMARY KEY(repo_id, pr_number, reviewer_email),
    FOREIGN KEY (repo_id, pr_number)
    REFERENCES repo_prs(repo_id, pr_number) ON DELETE CASCADE
);

-- A listing of all tags for all repos.
CREATE TABLE repo_commits (
    -- The sha, ex "699c34e735977eaed9d4f9aaf390956a5786c89e".
    commit_sha VARCHAR(40) PRIMARY KEY,

    repo_id BIGINT NOT NULL REFERENCES repos(repo_id) ON DELETE CASCADE,

    -- When the sha was committed.
    committed_date TIMESTAMP NOT NULL,

    -- The author's email. Only supports one (primary) author.
    author_email VARCHAR(255) NOT NULL,

    -- The PR this commit was associated with, if any. May be null.
    associated_pr_repo_id BIGINT,
    associated_pr_number INT,

    FOREIGN KEY (associated_pr_repo_id, associated_pr_number)
    REFERENCES repo_prs(repo_id, pr_number) ON DELETE SET NULL
);

-- Indexes for performance.
CREATE INDEX idx_repo_commits_repo_id_date ON repo_commits(repo_id, committed_date DESC);
CREATE INDEX idx_repo_commits_author_email ON repo_commits(author_email);
