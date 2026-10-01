package redis

import (
	"context"
	redis "github.com/redis/go-redis/v9"
	"shopnext-laos/internal/domain"
	"time"
)

type Limiter struct{ Client *redis.Client }

func New(url string) (*Limiter, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.DialTimeout = 3 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	opts.PoolSize = 20
	return &Limiter{redis.NewClient(opts)}, nil
}
func (r *Limiter) Ping(ctx context.Context) error { return r.Client.Ping(ctx).Err() }
func (r *Limiter) Close() error                   { return r.Client.Close() }

var counter = redis.NewScript(`local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return {n,redis.call('PTTL',KEYS[1])}`)

func (r *Limiter) Check(ctx context.Context, scope, key string, max int, window time.Duration) (time.Duration, error) {
	v, err := counter.Run(ctx, r.Client, []string{"shopnext:rate:" + scope + ":" + domain.Hash(key)}, window.Milliseconds()).Slice()
	if err != nil {
		return 0, domain.Fail("RATE_LIMITER_UNAVAILABLE", 503)
	}
	if v[0].(int64) > int64(max) {
		return time.Duration(v[1].(int64)) * time.Millisecond, nil
	}
	return 0, nil
}
