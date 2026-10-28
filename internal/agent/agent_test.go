package agent

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/store"
)

type failingPipeline struct{}

func (failingPipeline) Run(context.Context, deploys.Deploy) error {
	return errors.New("podman exploded")
}

func TestRunNextDeployFailsDeployWhenPipelineErrors(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	deployID, err := deploys.Enqueue(ctx, db, 1, deploys.Trigger{Type: deploys.TriggerManual}, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if err := runNextDeploy(ctx, db, failingPipeline{}, time.Now); err == nil {
		t.Fatal("pipeline error was swallowed")
	}

	var state, failedStep string
	if err := db.QueryRow("SELECT state, failed_step FROM deploys WHERE id = ?", deployID).Scan(&state, &failedStep); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || failedStep != "building" {
		t.Fatalf("deploy = %s at %q, want failed at building", state, failedStep)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('api', 'https://github.com/o/r', 'main', 1)"); err != nil {
		t.Fatal(err)
	}

	return db
}
