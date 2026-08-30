package connections

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type catalogPrismModelTestExecutor struct {
	queryRowCalls int
	rowError      error
}

func (*catalogPrismModelTestExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("unexpected Exec")
}

func (*catalogPrismModelTestExecutor) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected Query")
}

func (exec *catalogPrismModelTestExecutor) QueryRow(context.Context, string, ...any) pgx.Row {
	exec.queryRowCalls++
	return catalogPrismModelTestRow{err: exec.rowError}
}

type catalogPrismModelTestRow struct {
	err error
}

func (row catalogPrismModelTestRow) Scan(...any) error {
	return row.err
}

func TestLoadCatalogPrismModelOmittedSkipsLookup(t *testing.T) {
	exec := &catalogPrismModelTestExecutor{}

	model, err := loadCatalogPrismModel(context.Background(), exec, 1, nil)

	if err != nil || model != nil {
		t.Fatalf("omitted model_config_id = (%+v, %v), want (nil, nil)", model, err)
	}
	if exec.queryRowCalls != 0 {
		t.Fatalf("omitted model_config_id performed %d lookups, want 0", exec.queryRowCalls)
	}
}

func TestLoadCatalogPrismModelRejectsInvalidOrUnknownIdentity(t *testing.T) {
	testCases := []struct {
		name           string
		modelConfigID  int
		rowError       error
		wantStatus     int
		wantQueryCalls int
		wantDetail     string
	}{
		{name: "zero", modelConfigID: 0, wantStatus: http.StatusUnprocessableEntity, wantDetail: "model_config_id must be a positive integer when provided"},
		{name: "negative", modelConfigID: -7, wantStatus: http.StatusUnprocessableEntity, wantDetail: "model_config_id must be a positive integer when provided"},
		{name: "unknown", modelConfigID: 42, rowError: pgx.ErrNoRows, wantStatus: http.StatusNotFound, wantQueryCalls: 1, wantDetail: "Model configuration 42 does not exist in this profile"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			exec := &catalogPrismModelTestExecutor{rowError: testCase.rowError}

			model, err := loadCatalogPrismModel(context.Background(), exec, 1, &testCase.modelConfigID)

			if model != nil {
				t.Fatalf("model = %+v, want nil", model)
			}
			var domainErr *DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("error = %v, want DomainError", err)
			}
			if domainErr.StatusCode != testCase.wantStatus || domainErr.Detail != testCase.wantDetail {
				t.Fatalf("error = (%d, %q), want (%d, %q)", domainErr.StatusCode, domainErr.Detail, testCase.wantStatus, testCase.wantDetail)
			}
			if exec.queryRowCalls != testCase.wantQueryCalls {
				t.Fatalf("query calls = %d, want %d", exec.queryRowCalls, testCase.wantQueryCalls)
			}
		})
	}
}
