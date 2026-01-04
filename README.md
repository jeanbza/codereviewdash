# codereviewdash

codereviewdash is a service that indexes GitHub repositories and serves a
dashboard of PR statistics for your GitHub organization. It analyzes code review
culture by tracking metrics like PR coverage, time to review, and reviewer
participation.

## Running

### 0. Get a PAT

For GitHub Enterprise, create a Personal Access Token with the required
permissions:

1. Go to your GitHub Enterprise instance → Settings → Developer settings → Personal access tokens
2. Generate a new token with these scopes:
   - `repo` (Full control of private repositories)
   - `read:org` (Read org and team membership)

For fine-grained tokens, ensure read access to:
- Contents
- Metadata  
- Pull requests
- Commit statuses

### 1. Set up PostgreSQL

#### Option A: Docker (recommended for development)

```bash
export POSTGRES_USERNAME=postgres
export POSTGRES_PASSWORD=postgres
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export POSTGRES_DB=crdash

docker run \
    --name crdash-postgres \
    -e POSTGRES_USER=$POSTGRES_USERNAME \
    -e POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
    -e POSTGRES_DB=$POSTGRES_DB \
    -p "$POSTGRES_PORT:5432" \
    -d postgres:15
```

#### Option B: Native Installation

**On macOS (Homebrew):**
```bash
brew install postgresql@15
brew services start postgresql@15
export POSTGRES_USERNAME=$USER
export POSTGRES_PASSWORD=""
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export POSTGRES_DB=crdash
createdb $POSTGRES_DB
```

**On Linux:**
```bash
sudo apt-get install postgresql postgresql-contrib
export POSTGRES_USERNAME=postgres
export POSTGRES_PASSWORD=your_password_here
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5432
export POSTGRES_DB=crdash
sudo -u postgres createdb $POSTGRES_DB
```

### 2. Run Database Migrations

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
migrate -source file://migrations \
    -database "postgres://$POSTGRES_USERNAME:$POSTGRES_PASSWORD@$POSTGRES_HOST:$POSTGRES_PORT/$POSTGRES_DB?sslmode=disable" \
    up
```

**Note**: On macOS with Homebrew, the connection string will look like:
`postgres://yourusername:@localhost:5432/prstats?sslmode=disable` (notice the empty password after the colon)

### 3. Run the Indexer

```bash
go run ./cmd/indexer \
    -githubHostName=github.your-company.com \
    -githubAuthToken=your_github_token_here \
    -githubOrg=your-org-name
```

#### Filtering by Repository Prefix

To index only repositories matching a specific prefix (which significantly reduces GitHub API calls):

```bash
go run ./cmd/indexer \
    -githubHostName=github.your-company.com \
    -githubAuthToken=your_github_token_here \
    -githubOrg=your-org-name \
    -repoPrefix=cosmos-
```
