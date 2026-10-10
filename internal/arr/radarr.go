package arr

import "context"

func (c *client) Movies(ctx context.Context) ([]Movie, error) {
	return list[Movie](ctx, c, "/movie")
}
