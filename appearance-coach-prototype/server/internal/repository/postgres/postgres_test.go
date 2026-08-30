package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsForeignKeyViolation(t *testing.T) {
	fk := &pgconn.PgError{Code: "23503", ConstraintName: "reports_analysis_id_fkey"}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "matching constraint", err: fk, want: true},
		{name: "wrapped", err: fmt.Errorf("insert report: %w", fk), want: true},
		{name: "other constraint", err: &pgconn.PgError{Code: "23503", ConstraintName: "plans_report_id_fkey"}, want: false},
		{name: "unique violation", err: &pgconn.PgError{Code: "23505", ConstraintName: "reports_analysis_id_fkey"}, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isForeignKeyViolation(test.err, "reports_analysis_id_fkey"); got != test.want {
				t.Fatalf("isForeignKeyViolation(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}
