// Package db implements special indexing logic for an Postgres database.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	// TODO(jbarkhuysen): Consider switching to pgx instead.
	_ "github.com/lib/pq" // Postgres driver.
)

// A db handle with specialised logic for indexing.
type DB struct {
	db *sql.DB
}

func Connect(ctx context.Context, username, password, host string, port uint16, dbname string) (*sql.DB, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", username, password, host, port, dbname)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("error pinging db: %v", err)
	}

	return db, nil
}

func NewDB(db *sql.DB) *DB {
	return &DB{db: db}
}

type Repo struct {
	RepoID            int64  // Surrogate key for the repo
	OrgRepoName       string // Something like "corp/my-repo".
	DefaultBranchName string
}

// A commit for a repo.
type RepoCommit struct {
	SHA                string
	RepoID             int64
	Committed          time.Time
	AuthorEmail        string
	AssociatedPRRepoID *int64 // May be nil.
	AssociatedPRNumber *int   // May be nil.
}

// A PR for a repo.
type RepoPR struct {
	RepoID    int64                  // The repo this PR belongs to.
	Number    int                    // The PR number.
	Created   *time.Time             // When the PR was created.
	Merged    *time.Time             // When the PR was merged (nil if not merged).
	Reviewers []*RepoPRReviewerStats // The reviewers for the PR.
}

// Stats for a reviewer of a PR.
type RepoPRReviewerStats struct {
	ReviewerEmail string // The reviewer for the PR.
	NumComments   int    // The number of times they commented on the PR.
	Approved      bool   // Whether they approved the PR or not.
}

// Retrieves from the work queue whether it's time to re-index all repos.
func (d *DB) NextReindexAllReposWork(ctx context.Context, reindexTTL, reindexPeriod time.Duration) (shouldReindex bool, _ error) {
	query := `
UPDATE repo_indexing
SET indexing_began = NOW()
WHERE indexing_began + ($1 * INTERVAL '1 SECOND') < NOW()
AND indexing_finished + ($2 * INTERVAL '1 SECOND') < NOW();`
	id, err := d.db.ExecContext(ctx, query, int64(reindexTTL.Seconds()), int64(reindexPeriod.Seconds()))
	if err != nil {
		return false, fmt.Errorf("NextReindexAllReposWork:\nquery: %s\nerror: %v", query, err)
	}
	a, err := id.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("NextReindexAllReposWork: %v", err)
	}
	return a > 0, nil
}

// Retrieves from the work queue the next repo for which to re-index (both PRs and commits).
// workWasFound will be false if no work was found.
func (d *DB) NextReindexRepoWork(ctx context.Context, reindexTTL, reindexPeriod time.Duration) (repoID int64, repoToReindex, defaultBranchName string, workWasFound bool, _ error) {
	query := `
UPDATE repos
SET indexing_began = NOW()
WHERE repo_id = (
    SELECT repo_id
    FROM repos
    WHERE indexing_began + ($1 * INTERVAL '1 SECOND') < NOW()
    AND indexing_finished + ($2 * INTERVAL '1 SECOND') < NOW()
    ORDER BY indexing_finished ASC
    LIMIT 1
)
RETURNING repo_id, org_repo_name, default_branch_name;`

	row := d.db.QueryRowContext(ctx, query, int64(reindexTTL.Seconds()), int64(reindexPeriod.Seconds()))
	if row.Err() != nil {
		return 0, "", "", false, fmt.Errorf("NextReindexRepoWork:\nquery: %s\nerror: %v", query, row.Err())
	}
	var rID int64
	var rName, rBranch string
	if err := row.Scan(&rID, &rName, &rBranch); err != nil {
		if err == sql.ErrNoRows {
			return 0, "", "", false, nil
		}
		return 0, "", "", false, fmt.Errorf("NextReindexRepoWork: %v", err)
	}
	return rID, rName, rBranch, true, nil
}

// Store the given repos. Afterwards, they will be ready for repo tag indexing.
// Updates the RepoID field in each repo struct with the database-assigned ID.
//
// TODO(jbarkhuysen): The given orgRepoNames should be treated as authoratative.
// Any repos in GitHub not in this list should be deleted (and their repo tags).
func (d *DB) StoreRepos(ctx context.Context, repos []*Repo) error {
	if len(repos) == 0 {
		return fmt.Errorf("StoreRepos called with 0 repos")
	}

	// Insert or update each repo and get back the repo_id
	for _, repo := range repos {
		query := `
INSERT INTO repos (org_repo_name, default_branch_name)
VALUES ($1, $2)
ON CONFLICT (org_repo_name) 
DO UPDATE SET default_branch_name = EXCLUDED.default_branch_name
RETURNING repo_id;`

		var repoID int64
		err := d.db.QueryRowContext(ctx, query, repo.OrgRepoName, repo.DefaultBranchName).Scan(&repoID)
		if err != nil {
			return fmt.Errorf("StoreRepos for %s:\nquery: %s\nerror: %v", repo.OrgRepoName, query, err)
		}
		repo.RepoID = repoID
	}

	slog.Info(fmt.Sprintf("stored %d repos in database", len(repos)))

	return nil
}

func (d *DB) StoreRepoCommits(ctx context.Context, repoCommits []*RepoCommit) error {
	if len(repoCommits) == 0 {
		return nil // Nothing to store
	}

	var valueStrings []string
	var valueArgs []any
	const fieldCount = 6
	for i, commit := range repoCommits {
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d)",
			fieldCount*i+1, fieldCount*i+2, fieldCount*i+3, fieldCount*i+4, fieldCount*i+5, fieldCount*i+6))
		valueArgs = append(valueArgs, commit.SHA, commit.RepoID, commit.Committed, commit.AuthorEmail,
			commit.AssociatedPRRepoID, commit.AssociatedPRNumber)
	}

	query := fmt.Sprintf(`
INSERT INTO repo_commits (commit_sha, repo_id, committed_date, author_email, associated_pr_repo_id, associated_pr_number)
VALUES %s
ON CONFLICT (commit_sha) DO UPDATE SET
    repo_id = EXCLUDED.repo_id,
    committed_date = EXCLUDED.committed_date,
    author_email = EXCLUDED.author_email,
    associated_pr_repo_id = EXCLUDED.associated_pr_repo_id,
    associated_pr_number = EXCLUDED.associated_pr_number;`, strings.Join(valueStrings, ",\n\t"))

	if _, err := d.db.ExecContext(ctx, query, valueArgs...); err != nil {
		return fmt.Errorf("StoreRepoCommits:\nquery: %s\nerror: %v", query, err)
	}

	slog.Info(fmt.Sprintf("stored %d commits in database", len(repoCommits)))
	return nil
}

func (d *DB) StoreRepoPRs(ctx context.Context, repoPRs []*RepoPR) error {
	if len(repoPRs) == 0 {
		return nil // Nothing to store
	}

	// Use a transaction to ensure consistency
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("StoreRepoPRs: failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	// Insert PRs first
	var prValueStrings []string
	var prValueArgs []any
	const prFieldCount = 4
	for i, pr := range repoPRs {
		prValueStrings = append(prValueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d)",
			prFieldCount*i+1, prFieldCount*i+2, prFieldCount*i+3, prFieldCount*i+4))
		prValueArgs = append(prValueArgs, pr.RepoID, pr.Number, pr.Created, pr.Merged)
	}

	prQuery := fmt.Sprintf(`
INSERT INTO repo_prs (repo_id, pr_number, created, merged)
VALUES %s
ON CONFLICT (repo_id, pr_number) DO UPDATE SET
    created = EXCLUDED.created,
    merged = EXCLUDED.merged;`, strings.Join(prValueStrings, ",\n\t"))

	if _, err := tx.ExecContext(ctx, prQuery, prValueArgs...); err != nil {
		return fmt.Errorf("StoreRepoPRs PRs:\nquery: %s\nerror: %v", prQuery, err)
	}

	// Delete existing reviewers for these PRs to avoid stale data
	for _, pr := range repoPRs {
		deleteQuery := `DELETE FROM pr_reviewers WHERE repo_id = $1 AND pr_number = $2`
		if _, err := tx.ExecContext(ctx, deleteQuery, pr.RepoID, pr.Number); err != nil {
			return fmt.Errorf("StoreRepoPRs delete existing reviewers for PR %d: %v", pr.Number, err)
		}
	}

	// Insert reviewers
	var allReviewers []*struct {
		RepoID        int64
		PRNumber      int
		ReviewerEmail string
		NumComments   int
		Approved      bool
	}

	for _, pr := range repoPRs {
		for _, reviewer := range pr.Reviewers {
			allReviewers = append(allReviewers, &struct {
				RepoID        int64
				PRNumber      int
				ReviewerEmail string
				NumComments   int
				Approved      bool
			}{
				RepoID:        pr.RepoID,
				PRNumber:      pr.Number,
				ReviewerEmail: reviewer.ReviewerEmail,
				NumComments:   reviewer.NumComments,
				Approved:      reviewer.Approved,
			})
		}
	}

	if len(allReviewers) > 0 {
		var reviewerValueStrings []string
		var reviewerValueArgs []any
		const reviewerFieldCount = 5
		for i, reviewer := range allReviewers {
			reviewerValueStrings = append(reviewerValueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d)",
				reviewerFieldCount*i+1, reviewerFieldCount*i+2, reviewerFieldCount*i+3, reviewerFieldCount*i+4, reviewerFieldCount*i+5))
			reviewerValueArgs = append(reviewerValueArgs, reviewer.RepoID, reviewer.PRNumber, reviewer.ReviewerEmail,
				reviewer.NumComments, reviewer.Approved)
		}

		reviewerQuery := fmt.Sprintf(`
INSERT INTO pr_reviewers (repo_id, pr_number, reviewer_email, num_comments, approved)
VALUES %s;`, strings.Join(reviewerValueStrings, ",\n\t"))

		if _, err := tx.ExecContext(ctx, reviewerQuery, reviewerValueArgs...); err != nil {
			return fmt.Errorf("StoreRepoPRs reviewers:\nquery: %s\nerror: %v", reviewerQuery, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("StoreRepoPRs: failed to commit transaction: %v", err)
	}

	totalReviewers := 0
	for _, pr := range repoPRs {
		totalReviewers += len(pr.Reviewers)
	}
	slog.Info(fmt.Sprintf("stored %d PRs with %d total reviewers in database", len(repoPRs), totalReviewers))
	return nil
}
