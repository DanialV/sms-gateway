package redis

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"sms-gateway/internal/domain"
)

const (
	addChunkSize = 500
	wakeBatch    = 500
)

// New customers join at the lowest score: they get a turn soon without jumping ahead of everyone.
var addScript = goredis.NewScript(`
local head = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
local score = head[2] or 0
local added = 0
for _, member in ipairs(ARGV) do
  added = added + redis.call('ZADD', KEYS[1], 'NX', score, member)
end
return added
`)

// Take the lowest score and bump it instead of popping, so a busy customer keeps its place in the ring.
var nextScript = goredis.NewScript(`
local head = redis.call('ZRANGE', KEYS[1], 0, 0)
if #head == 0 then
  return false
end
redis.call('ZINCRBY', KEYS[1], 1, head[1])
return head[1]
`)

// Move customers whose alarm time has passed back into the ready set, at the lowest score.
var wakeScript = goredis.NewScript(`
local due = redis.call('ZRANGE', KEYS[1], '-inf', ARGV[1], 'BYSCORE', 'LIMIT', 0, ARGV[2])
if #due == 0 then
  return 0
end
local head = redis.call('ZRANGE', KEYS[2], 0, 0, 'WITHSCORES')
local score = head[2] or 0
for _, member in ipairs(due) do
  redis.call('ZADD', KEYS[2], 'NX', score, member)
end
redis.call('ZREM', KEYS[1], unpack(due))
return #due
`)

type Scheduler struct {
	rdb goredis.UniversalClient
}

func NewScheduler(rdb goredis.UniversalClient) *Scheduler {
	return &Scheduler{rdb: rdb}
}

func (s *Scheduler) Add(ctx context.Context, lane domain.Lane, customerIDs ...uint32) error {
	for chunk := range slices.Chunk(customerIDs, addChunkSize) {
		if err := addScript.Run(ctx, s.rdb, []string{readyKey(lane)}, members(chunk)...).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) Next(ctx context.Context, lane domain.Lane) (uint32, bool, error) {
	member, err := nextScript.Run(ctx, s.rdb, []string{readyKey(lane)}).Text()
	if errors.Is(err, goredis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	id, err := strconv.ParseUint(member, 10, 32)
	return uint32(id), err == nil, err
}

func (s *Scheduler) Remove(ctx context.Context, lane domain.Lane, customerID uint32) error {
	return s.rdb.ZRem(ctx, readyKey(lane), strconv.FormatUint(uint64(customerID), 10)).Err()
}

func (s *Scheduler) Delay(ctx context.Context, lane domain.Lane, customerID uint32, until time.Time) error {
	member := goredis.Z{Score: float64(until.UnixMilli()), Member: strconv.FormatUint(uint64(customerID), 10)}
	return s.rdb.ZAddLT(ctx, delayedKey(lane), member).Err()
}

func (s *Scheduler) WakeDue(ctx context.Context, lane domain.Lane, now time.Time) error {
	keys := []string{delayedKey(lane), readyKey(lane)}
	return wakeScript.Run(ctx, s.rdb, keys, now.UnixMilli(), wakeBatch).Err()
}

func (s *Scheduler) Size(ctx context.Context, lane domain.Lane) (int64, error) {
	return s.rdb.ZCard(ctx, readyKey(lane)).Result()
}

func readyKey(lane domain.Lane) string {
	return "sms:ready:" + string(lane)
}

func delayedKey(lane domain.Lane) string {
	return "sms:delayed:" + string(lane)
}

func members(ids []uint32) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatUint(uint64(id), 10)
	}
	return out
}
