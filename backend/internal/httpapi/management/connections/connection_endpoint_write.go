package connections

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (s *Service) resolveCreateEndpoint(ctx context.Context, tx pgx.Tx, profileID int, endpointID *int, inline *EndpointCreateRequest) (endpointRecord, error) {
	if endpointID != nil {
		endpoint, found, err := loadProfileEndpointRecord(ctx, tx, profileID, *endpointID)
		if err != nil {
			return endpointRecord{}, err
		}
		if !found {
			return endpointRecord{}, &DomainError{StatusCode: http.StatusNotFound, Detail: "Endpoint not found"}
		}
		return endpoint, nil
	}
	if inline != nil {
		return s.createInlineEndpoint(ctx, tx, profileID, endpointCreateRequest{Name: inline.Name, BaseURL: inline.BaseURL, APIKey: inline.APIKey})
	}
	return endpointRecord{}, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "Exactly one of endpoint_id or endpoint_create is required"}
}

// createInlineEndpoint adapts the composite-create request to the existing
// HTTP-neutral owner writer so endpoint validation, secret metadata, and insert
// semantics remain owned by the shared writer path.
func (s *Service) createInlineEndpoint(ctx context.Context, tx pgx.Tx, profileID int, inline endpointCreateRequest) (endpointRecord, error) {
	return insertWriterInlineEndpoint(ctx, tx, profileID, InlineEndpointCreate{
		Name: inline.Name, BaseURL: inline.BaseURL, APIKey: inline.APIKey,
	}, s.secretEncryptionKey, s.nowUTC)
}
