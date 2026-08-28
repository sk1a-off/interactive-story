package imagestorage

import "context"

type Storage interface {
	Save(context.Context, string, string, []byte) (string, error)
}
