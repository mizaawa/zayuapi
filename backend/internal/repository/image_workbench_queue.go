package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

var startWorkbenchScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local task = cjson.decode(raw)
if task.user_id ~= tonumber(ARGV[2]) or task.api_key_id ~= tonumber(ARGV[3]) or task.status ~= 'queued' then return -1 end
local now = tonumber(ARGV[4])
if tonumber(task.queue_lease_until or 0) <= now or tonumber(task.queue_deadline or 0) <= now or task.expires_at <= now then return -2 end
task.queue_lease_until = math.min(now + tonumber(ARGV[5]), task.queue_deadline)
redis.call('SET', KEYS[1], cjson.encode(task), 'PX', (task.expires_at - now) * 1000)
redis.call('ZADD', KEYS[5], task.queue_lease_until, ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
-- Remove orphaned heads without claiming them; the lease sweeper records failure.
while true do
  local head = redis.call('ZRANGE', KEYS[3], 0, 0)[1]
  if not head then return -1 end
  local headRaw = redis.call('GET', ARGV[9] .. head)
  local pending = headRaw and cjson.decode(headRaw)
  if pending and pending.status == 'queued' and tonumber(pending.queue_lease_until or 0) > now and tonumber(pending.queue_deadline or 0) > now then break end
  redis.call('ZREM', KEYS[3], head)
end
local limit = tonumber(ARGV[6])
if limit > 0 then
  local available = limit - redis.call('ZCARD', KEYS[2])
  local position = redis.call('ZRANK', KEYS[3], ARGV[1])
  if not position then return -1 end
  if available <= 0 or position >= available then return 0 end
end
task.status = 'processing'
task.started_at = now
task.queue_lease_until = nil
task.queue_deadline = nil
task.expires_at = now + tonumber(ARGV[7])
redis.call('SET', KEYS[1], cjson.encode(task), 'PX', tonumber(ARGV[7]) * 1000)
redis.call('ZREM', KEYS[3], ARGV[1])
redis.call('ZREM', KEYS[5], ARGV[1])
redis.call('ZADD', KEYS[2], task.expires_at, ARGV[1])
redis.call('ZADD', KEYS[4], task.expires_at, ARGV[1])
redis.call('ZADD', KEYS[6], task.expires_at, ARGV[8])
redis.call('EXPIRE', KEYS[2], math.max(86400, tonumber(ARGV[7])))
redis.call('EXPIRE', KEYS[4], math.max(86400, tonumber(ARGV[7])))
return 1
`)

func (s *imageTaskStore) TryStartWorkbench(ctx context.Context, owner service.ImageTaskOwner, id string, now time.Time, ttl time.Duration, limit int) (bool, error) {
	task, err := s.Get(ctx, id)
	if err != nil {
		return false, err
	}
	cleanup, err := json.Marshal(service.ImageWorkbenchCleanup{ID: id, UserEmail: task.UserEmail, Storage: task.WorkbenchStorage})
	if err != nil {
		return false, err
	}
	result, err := startWorkbenchScript.Run(ctx, s.rdb,
		[]string{imageTaskKey(id), workbenchActiveIndex(owner.UserID), workbenchQueueIndex(owner.UserID), workbenchTaskIndex(owner.UserID), workbenchQueueLeases, workbenchCleanupIndex},
		id, owner.UserID, owner.APIKeyID, now.Unix(), int64(service.ImageWorkbenchQueueLease.Seconds()), limit, int64(ttl.Seconds()), string(cleanup), imageTaskKeyPrefix).Int()
	if err != nil {
		return false, err
	}
	switch result {
	case -1:
		return false, service.ErrImageTaskNotFound
	case -2:
		return false, service.ErrImageWorkbenchQueueExpired
	}
	return result == 1, nil
}

func (s *imageTaskStore) ExpiredWorkbenchQueue(ctx context.Context, now time.Time) ([]*service.ImageTaskRecord, error) {
	ids, err := s.rdb.ZRangeByScore(ctx, workbenchQueueLeases, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Offset: 0, Count: 100,
	}).Result()
	if err != nil {
		return nil, err
	}
	tasks := make([]*service.ImageTaskRecord, 0, len(ids))
	for _, id := range ids {
		task, err := s.Get(ctx, id)
		if err == service.ErrImageTaskNotFound || (err == nil && task.Status != service.ImageTaskStatusQueued) {
			if err := s.rdb.ZRem(ctx, workbenchQueueLeases, id).Err(); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *imageTaskStore) FailExpiredWorkbenchQueue(ctx context.Context, task *service.ImageTaskRecord, now time.Time, ttl time.Duration) error {
	return s.saveWorkbench(ctx, task, ttl, now.Unix())
}
