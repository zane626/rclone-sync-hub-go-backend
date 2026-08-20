package repository

import (
	"errors"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestClassifyConstraintErrorRecognizesDuplicateKey(t *testing.T) {
	err := classifyConstraintError(&mysqlDriver.MySQLError{Number: 1062, Message: "duplicate"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate key was not classified as conflict: %v", err)
	}
	original := errors.New("connection lost")
	if got := classifyConstraintError(original); got != original {
		t.Fatalf("non-constraint error was changed: %v", got)
	}
}
