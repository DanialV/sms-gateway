package redis

import (
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"sms-gateway/internal/domain"
)

const wakeJitter = 25 * time.Millisecond

// One-second window shared by all workers; normal has its own lower cap so express keeps its reserved share.
var allowScript = goredis.NewScript(`
local total = tonumber(redis.call('GET', KEYS[1]) or '0')
if total >= tonumber(ARGV[1]) then
  return 0
end
if KEYS[2] then
  local normal = tonumber(redis.call('GET', KEYS[2]) or '0')
  if normal >= tonumber(ARGV[2]) then
    return 0
  end
  redis.call('INCR', KEYS[2])
  redis.call('EXPIRE', KEYS[2], 2)
end
redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], 2)
return 1
`)

type Limiter struct {
	rdb       goredis.UniversalClient
	perSecond int
	normalCap int
}

func NewLimiter(rdb goredis.UniversalClient, perSecond, expressReservedPercent int) *Limiter {
	return &Limiter{
		rdb:       rdb,
		perSecond: perSecond,
		normalCap: perSecond * (100 - expressReservedPercent) / 100,
	}
}

func (l *Limiter) Wait(ctx context.Context, lane domain.Lane) error {
	if l.perSecond <= 0 {
		return nil
	}
	for {
		now := time.Now()
		allowed, err := l.allow(ctx, lane, now.Unix())
		if err != nil || allowed {
			return err
		}
		nextWindow := now.Truncate(time.Second).Add(time.Second + rand.N(wakeJitter))
		if err := sleepUntil(ctx, nextWindow); err != nil {
			return err
		}
	}
}

func (l *Limiter) allow(ctx context.Context, lane domain.Lane, window int64) (bool, error) {
	key := "sms:rate:" + strconv.FormatInt(window, 10)
	keys := []string{key}
	if lane == domain.LaneNormal {
		keys = append(keys, key+":normal")
	}
	n, err := allowScript.Run(ctx, l.rdb, keys, l.perSecond, l.normalCap).Int()
	return n == 1, err
}

func sleepUntil(ctx context.Context, at time.Time) error {
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
