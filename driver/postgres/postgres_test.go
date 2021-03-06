package postgres

import (
	"database/sql"
	"os"
	"reflect"
	"testing"

	"github.com/chibimi/migrate/file"
	"github.com/chibimi/migrate/migrate/direction"
	pipep "github.com/chibimi/migrate/pipe"
)

// TestMigrate runs some additional tests on Migrate().
// Basic testing is already done in migrate/migrate_test.go
func TestMigrate(t *testing.T) {
	host := os.Getenv("POSTGRES_PORT_5432_TCP_ADDR")
	port := os.Getenv("POSTGRES_PORT_5432_TCP_PORT")
	driverUrl := "postgres://postgres@" + host + ":" + port + "/template1?sslmode=disable"

	// prepare clean database
	connection, err := sql.Open("postgres", driverUrl)
	if err != nil {
		t.Fatal(err)
	}

	dropTestTables(t, connection)

	migrate(t, driverUrl)

	dropTestTables(t, connection)

	// Make an old-style `int` version column that we'll have to upgrade.
	_, err = connection.Exec("CREATE TABLE IF NOT EXISTS " + tableName + " (version int not null primary key)")
	if err != nil {
		t.Fatal(err)
	}

	migrate(t, driverUrl)
}

func migrate(t *testing.T, driverUrl string) {
	d := &Driver{}
	if err := d.Initialize(driverUrl); err != nil {
		t.Fatal(err)
	}

	// testing idempotency: second call should be a no-op, since table already exists
	if err := d.Initialize(driverUrl); err != nil {
		t.Fatal(err)
	}

	files := []file.File{
		{
			Path:      "/foobar",
			FileName:  "20060102150405_foobar.up.sql",
			Version:   20060102150405,
			Name:      "foobar",
			Direction: direction.Up,
			Content: []byte(`
				CREATE TABLE yolo (
					id serial not null primary key
				);
				CREATE TYPE colors AS ENUM (
					'red',
					'green'
				);
			`),
		},
		{
			Path:      "/foobar",
			FileName:  "20060102150405_foobar.down.sql",
			Version:   20060102150405,
			Name:      "foobar",
			Direction: direction.Down,
			Content: []byte(`
				DROP TABLE yolo;
			`),
		},
		{
			Path:      "/foobar",
			FileName:  "20060102150406_foobar.up.sql",
			Version:   20060102150406,
			Name:      "foobar",
			Direction: direction.Up,
			Content: []byte(`-- disable_ddl_transaction
				ALTER TYPE colors ADD VALUE 'blue' AFTER 'red';
			`),
		},
		{
			Path:      "/foobar",
			FileName:  "20060102150406_foobar.down.sql",
			Version:   20060102150406,
			Name:      "foobar",
			Direction: direction.Down,
			Content: []byte(`
				DROP TYPE colors;
			`),
		},
		{
			Path:      "/foobar",
			FileName:  "20060102150407_foobar.up.sql",
			Version:   20060102150407,
			Name:      "foobar",
			Direction: direction.Up,
			Content: []byte(`
				CREATE TABLE error (
					id THIS WILL CAUSE AN ERROR
				)
			`),
		},
	}

	// should create table yolo
	pipe := pipep.New()
	go d.Migrate(files[0], pipe)
	errs := pipep.ReadErrors(pipe)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	version, err := d.Version()
	if err != nil {
		t.Fatal(err)
	}

	if version != 20060102150405 {
		t.Errorf("Expected version to be: %d, got: %d", 20060102150405, version)
	}

	// Check versions applied in DB
	expectedVersions := file.Versions{20060102150405}
	versions, err := d.Versions()
	if err != nil {
		t.Errorf("Could not fetch versions: %s", err)
	}

	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions to be: %v, got: %v", expectedVersions, versions)
	}

	// should alter type colors
	pipe = pipep.New()
	go d.Migrate(files[2], pipe)
	errs = pipep.ReadErrors(pipe)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	colors := []string{}
	expectedColors := []string{"red", "blue", "green"}
	d.db.Select(&colors, "SELECT unnest(enum_range(NULL::colors));")
	if !reflect.DeepEqual(colors, expectedColors) {
		t.Errorf("Expected colors enum to be %q, got %q\n", expectedColors, colors)
	}

	pipe = pipep.New()
	go d.Migrate(files[3], pipe)
	errs = pipep.ReadErrors(pipe)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	pipe = pipep.New()
	go d.Migrate(files[1], pipe)
	errs = pipep.ReadErrors(pipe)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	pipe = pipep.New()
	go d.Migrate(files[4], pipe)
	errs = pipep.ReadErrors(pipe)
	if len(errs) == 0 {
		t.Error("Expected test case to fail")
	}

	// Check versions applied in DB
	expectedVersions = file.Versions{}
	versions, err = d.Versions()
	if err != nil {
		t.Errorf("Could not fetch versions: %s", err)
	}

	if !reflect.DeepEqual(versions, expectedVersions) {
		t.Errorf("Expected versions to be: %v, got: %v", expectedVersions, versions)
	}

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

}

func dropTestTables(t *testing.T, db *sql.DB) {
	if _, err := db.Exec(`
				DROP TYPE IF EXISTS colors;
				DROP TABLE IF EXISTS yolo;
				DROP TABLE IF EXISTS ` + tableName + `;`); err != nil {
		t.Fatal(err)
	}

}
