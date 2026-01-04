package db_test

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jeanbza/codereviewdash/internal/db"

	_ "github.com/lib/pq" // Postgres driver
)

type reindexWorkerTestCase struct {
	name                 string
	lastIndexingBegan    time.Time
	lastIndexingFinished time.Time
	reindexTTL           time.Duration
	reindexPeriod        time.Duration // We should reindex after this period of time.
	expectReindex        bool
}

var reindexWorkerTestCases = []*reindexWorkerTestCase{
	{
		// We re-indexed long ago: we should do so again.
		name:                 "beyond reindex period",
		lastIndexingBegan:    time.Now().Add(-24 * time.Hour),
		lastIndexingFinished: time.Now().Add(-24 * time.Hour),
		reindexTTL:           time.Minute,
		reindexPeriod:        time.Hour,
		expectReindex:        true,
	},
	{
		// We re-indexed long ago, but another worker is busy re-indexing: don't re-index.
		name:                 "beyond reindex period but another worker busy",
		lastIndexingBegan:    time.Now().Add(-1 * time.Minute), // The other worker only started 1m ago, and has 5m: give it more time.
		lastIndexingFinished: time.Now().Add(-24 * time.Hour),
		reindexTTL:           5 * time.Minute,
		reindexPeriod:        time.Hour,
		expectReindex:        false,
	},
	{
		// We re-indexed long ago, but another worker is busy re-indexing: don't re-index.
		name:                 "beyond reindex period and another worker stalled",
		lastIndexingBegan:    time.Now().Add(-6 * time.Minute), // The other worker only started 6m ago, and has 5m: it's stalled, so take over.
		lastIndexingFinished: time.Now().Add(-24 * time.Hour),
		reindexTTL:           5 * time.Minute,
		reindexPeriod:        time.Hour,
		expectReindex:        true,
	},
	{
		// We've re-indexed recently: no point doing so again.
		name:                 "within reindex period",
		lastIndexingBegan:    time.Now().Add(-10 * time.Minute),
		lastIndexingFinished: time.Now().Add(-10 * time.Minute),
		reindexTTL:           time.Minute,
		reindexPeriod:        time.Hour,
		expectReindex:        false,
	},
	{
		// We're beyond the re-indexing TTL. But, since we're still within the re-indexing period, no need to re-index.
		name:                 "within reindex period despite recent start",
		lastIndexingBegan:    time.Now().Add(-10 * time.Minute),
		lastIndexingFinished: time.Now().Add(-10 * time.Minute),
		reindexTTL:           time.Second, // The last re-indexing worker had 1s to finish, and it's far beyond that TTL.
		reindexPeriod:        time.Hour,
		expectReindex:        false,
	},
}

func TestNextReindexAllReposWork_Basic(t *testing.T) {
	sutDB, sqlDB := setupDB(t)

	for _, tc := range reindexWorkerTestCases {
		t.Run(tc.name, func(t *testing.T) {
			resetTables(t, sqlDB)
			setAllReposIndexing(t, sqlDB, time.Now().Add(-24*time.Hour), time.Now().Add(-24*time.Hour))
			shouldReindex, err := sutDB.NextReindexAllReposWork(t.Context(), 5*time.Minute, 24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := shouldReindex, true; got != want {
				t.Errorf("expected shouldReindex=%v, got %v", want, got)
			}
		})
	}
}

func setupDB(t *testing.T) (*db.DB, *sql.DB) {
	// Check if required environment variables are set
	if os.Getenv("POSTGRES_USERNAME") == "" {
		t.Skip("skipping database tests: POSTGRES_USERNAME not set. Set POSTGRES_USERNAME, POSTGRES_PASSWORD, POSTGRES_HOST, POSTGRES_PORT, and POSTGRES_DB environment variables to run database tests.")
	}

	username, password, host, port, dbname, err := postgresDetails()
	if err != nil {
		t.Fatalf("failed to get postgres details: %v", err)
	}

	sqlDB, err := db.Connect(t.Context(), username, password, host, port, dbname)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	sutDB := db.NewDB(sqlDB)

	resetTables(t, sqlDB)

	return sutDB, sqlDB
}
