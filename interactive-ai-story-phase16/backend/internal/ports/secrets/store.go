package secrets

import "context"

type Store interface {
	Set(context.Context, string, string) error
	Has(context.Context, string) bool
	Get(context.Context, string) (string, bool)
}
