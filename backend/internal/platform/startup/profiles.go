package startup

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s Service) seedProfileInvariants(ctx context.Context, conn *pgx.Conn) error {
	return pgxutil.InTx(ctx, conn, "startup", func(tx pgx.Tx) error {
		_, err := profiledomain.EnsureInvariants(ctx, tx, s.timestamp)
		return err
	})
}
