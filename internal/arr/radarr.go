package arr

import "context"

func (c *client) Movies(ctx context.Context) ([]Movie, error) {
	return get[[]Movie](ctx, c, "/movie", "")
}
