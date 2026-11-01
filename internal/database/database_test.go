package database_test

import (
	"context"
	"sync"
	"testing"

	"github.com/aethera-deeptech/backend/internal/database"
	"github.com/aethera-deeptech/backend/internal/database/dbtest"
)

func TestMigrateCreatesTheBusinessesTable(t *testing.T) {
	pool := dbtest.New(t)

	var tariff *int

	var name *string

	err := pool.QueryRow(context.Background(),
		"INSERT INTO businesses DEFAULT VALUES RETURNING name, tariff_rp_per_kwh").Scan(&name, &tariff)
	if err != nil {
		t.Fatalf("insert with the defaults failed %v", err)
	}

	if name != nil || tariff != nil {
		t.Fatalf("a new business has name %v and tariff %v, want both null", name, tariff)
	}
}

func TestBusinessesRefuseValuesOutsideTheRules(t *testing.T) {
	pool := dbtest.New(t)

	tests := map[string]string{
		"tariff of zero":        "INSERT INTO businesses (tariff_rp_per_kwh) VALUES (0)",
		"unknown work days":     "INSERT INTO businesses (work_days) VALUES ('weekends')",
		"end before start":      "INSERT INTO businesses (work_start, work_end) VALUES ('17:00', '08:00')",
		"unknown sensitivity":   "INSERT INTO businesses (anomaly_sensitivity) VALUES ('ekstrem')",
		"empty name":            "INSERT INTO businesses (name) VALUES ('')",
		"name over 80 letters":  "INSERT INTO businesses (name) SELECT repeat('a', 81)",
		"empty production unit": "INSERT INTO businesses (production_unit) VALUES ('')",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(context.Background(), query); err == nil {
				t.Fatal("the database accepted the row, want a check violation")
			}
		})
	}
}

func TestMigrateTwiceAndFromTwoCopiesAtOnce(t *testing.T) {
	pool := dbtest.New(t)

	url := pool.Config().ConnString()

	var wg sync.WaitGroup

	errs := make(chan error, 3)

	for range 3 {
		wg.Go(func() { errs <- database.Migrate(url) })
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("a migrate on an up to date database failed %v", err)
		}
	}
}

func TestOpenRefusesABrokenURL(t *testing.T) {
	if _, err := database.Open(context.Background(), "postgres://%zz", 4); err == nil {
		t.Fatal("a broken url was accepted")
	}
}
