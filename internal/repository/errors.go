package repository

import (
	"errors"
	"fmt"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// ErrNotFound is returned when a repository lookup did not find a row. Keeping
// this distinct from transport/database failures lets the API return an
// accurate 404 without hiding an outage as a missing resource.
var ErrNotFound = errors.New("repository: record not found")

// ErrConflict identifies a uniqueness/constraint collision caused by a
// concurrent or duplicate request rather than a database outage.
var ErrConflict = errors.New("repository: constraint conflict")

func classifyConstraintError(err error) error {
	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}
