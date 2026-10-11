package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

type videoWorkbenchStore struct{ rdb *redis.Client }

const videoTaskPrefix = "video_workbench:task:"
const videoLeases = "video_workbench:leases"
const videoCleanup = "video_workbench:cleanup"

func NewVideoWorkbenchStore(rdb *redis.Client) service.VideoWorkbenchStore {
	return &videoWorkbenchStore{rdb: rdb}
}

func videoKeys(task *service.VideoWorkbenchRecord) []string {
	owner := strconv.FormatInt(task.UserID, 10)
	return []string{videoTaskPrefix + task.ID, "video_workbench:tasks:" + owner, "video_workbench:active:" + owner, "video_workbench:queued:" + owner, videoLeases, videoCleanup}
}

var videoCreateScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[4], '-inf', ARGV[1])
if redis.call('ZCARD', KEYS[4]) >= tonumber(ARGV[5]) then return 0 end
if not redis.call('SET', KEYS[1], ARGV[3], 'PX', ARGV[4], 'NX') then return 0 end
redis.call('ZADD', KEYS[2], ARGV[6], ARGV[2])
redis.call('ZADD', KEYS[4], ARGV[7], ARGV[2])
redis.call('ZADD', KEYS[5], ARGV[7], ARGV[2])
redis.call('ZADD', KEYS[6], ARGV[6], ARGV[2])
redis.call('PEXPIRE', KEYS[2], math.max(86400000, tonumber(ARGV[4])))
redis.call('PEXPIRE', KEYS[4], math.max(86400000, tonumber(ARGV[4])))
return 1
`)

func (s *videoWorkbenchStore) Create(ctx context.Context, task *service.VideoWorkbenchRecord, ttl time.Duration) error {
	body, err := json.Marshal(task)
	if err != nil {
		return err
	}
	n, err := videoCreateScript.Run(ctx, s.rdb, videoKeys(task), time.Now().Unix(), task.ID, body, ttl.Milliseconds(), service.VideoWorkbenchQueueLimit, task.ExpiresAt, task.LeaseUntil).Int()
	if err != nil {
		return service.ErrVideoTaskUnavailable.WithCause(err)
	}
	if n == 0 {
		return service.ErrVideoQueueFull
	}
	return nil
}

func (s *videoWorkbenchStore) Get(ctx context.Context, id string) (*service.VideoWorkbenchRecord, error) {
	if !service.ValidVideoWorkbenchTaskID(id) {
		return nil, service.ErrVideoTaskNotFound
	}
	body, err := s.rdb.Get(ctx, videoTaskPrefix+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, service.ErrVideoTaskUnavailable.WithCause(err)
	}
	var task service.VideoWorkbenchRecord
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, service.ErrVideoTaskUnavailable.WithCause(err)
	}
	return &task, nil
}

func (s *videoWorkbenchStore) records(ctx context.Context, ids []string) ([]*service.VideoWorkbenchRecord, error) {
	result := make([]*service.VideoWorkbenchRecord, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = videoTaskPrefix + id
	}
	values, err := s.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		if raw, ok := value.(string); ok {
			var task service.VideoWorkbenchRecord
			if err := json.Unmarshal([]byte(raw), &task); err != nil {
				return nil, err
			}
			result = append(result, &task)
		}
	}
	return result, nil
}

func (s *videoWorkbenchStore) List(ctx context.Context, userID int64, now time.Time) ([]*service.VideoWorkbenchRecord, error) {
	key := "video_workbench:tasks:" + strconv.FormatInt(userID, 10)
	if err := s.rdb.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now.Unix(), 10)).Err(); err != nil {
		return nil, err
	}
	ids, err := s.rdb.ZRevRange(ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	return s.records(ctx, ids)
}

var videoStartScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local task = cjson.decode(raw)
if task.status ~= 'queued' or task.lease_until <= tonumber(ARGV[1]) or task.deadline <= tonumber(ARGV[1]) then return -1 end
task.lease_until = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', ARGV[1])
local started = redis.call('ZCARD', KEYS[3]) < tonumber(ARGV[4])
if started then
  task.status = 'processing'
  task.started_at = tonumber(ARGV[1])
  task.deadline = tonumber(ARGV[5])
  task.expires_at = tonumber(ARGV[6])
  redis.call('ZADD', KEYS[3], ARGV[3], ARGV[2])
  redis.call('ZREM', KEYS[4], ARGV[2])
  redis.call('ZADD', KEYS[2], ARGV[6], ARGV[2])
  redis.call('ZADD', KEYS[6], ARGV[6], ARGV[2])
else
  redis.call('ZADD', KEYS[4], ARGV[3], ARGV[2])
end
redis.call('SET', KEYS[1], cjson.encode(task), 'EX', math.max(1, task.expires_at - tonumber(ARGV[1])))
redis.call('ZADD', KEYS[5], ARGV[3], ARGV[2])
redis.call('EXPIRE', KEYS[3], 86400)
if started then return 1 else return 0 end
`)

func (s *videoWorkbenchStore) TryStart(ctx context.Context, task *service.VideoWorkbenchRecord, now time.Time, retention time.Duration, limit int) (bool, error) {
	deadline := now.Add(service.VideoWorkbenchExecutionTimeout).Unix()
	expires := now.Add(service.VideoWorkbenchExecutionTimeout + retention).Unix()
	lease := now.Add(service.VideoWorkbenchLease).Unix()
	n, err := videoStartScript.Run(ctx, s.rdb, videoKeys(task), now.Unix(), task.ID, lease, limit, deadline, expires).Int()
	if err != nil {
		return false, err
	}
	if n < 0 {
		return false, service.ErrVideoTaskNotFound
	}
	if n == 1 {
		task.Status, task.StartedAt, task.LeaseUntil, task.Deadline, task.ExpiresAt = "processing", now.Unix(), lease, deadline, expires
	}
	return n == 1, nil
}

var videoHeartbeatScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local task = cjson.decode(raw)
if task.status ~= 'processing' or task.lease_until <= tonumber(ARGV[1]) or task.deadline <= tonumber(ARGV[1]) then return 0 end
task.lease_until = math.min(task.deadline, tonumber(ARGV[3]))
redis.call('SET', KEYS[1], cjson.encode(task), 'KEEPTTL')
redis.call('ZADD', KEYS[3], task.lease_until, ARGV[2])
redis.call('ZADD', KEYS[5], task.lease_until, ARGV[2])
return 1
`)

func (s *videoWorkbenchStore) Heartbeat(ctx context.Context, task *service.VideoWorkbenchRecord, now time.Time) error {
	n, err := videoHeartbeatScript.Run(ctx, s.rdb, videoKeys(task), now.Unix(), task.ID, now.Add(service.VideoWorkbenchLease).Unix()).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrVideoTaskNotFound
	}
	return nil
}

var videoFinishScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local current = cjson.decode(raw)
if current.status ~= 'processing' and current.status ~= 'queued' then return 0 end
if tonumber(ARGV[5]) > 0 and current.lease_until > tonumber(ARGV[5]) and current.deadline > tonumber(ARGV[5]) then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('ZADD', KEYS[2], ARGV[4], ARGV[1])
redis.call('PEXPIRE', KEYS[2], math.max(86400000, tonumber(ARGV[3])))
redis.call('ZREM', KEYS[3], ARGV[1])
redis.call('ZREM', KEYS[4], ARGV[1])
redis.call('ZREM', KEYS[5], ARGV[1])
redis.call('ZADD', KEYS[6], ARGV[4], ARGV[1])
return 1
`)

func (s *videoWorkbenchStore) Finish(ctx context.Context, task *service.VideoWorkbenchRecord, ttl time.Duration, expiredBefore int64) (bool, error) {
	body, err := json.Marshal(task)
	if err != nil {
		return false, err
	}
	n, err := videoFinishScript.Run(ctx, s.rdb, videoKeys(task), task.ID, body, ttl.Milliseconds(), task.ExpiresAt, expiredBefore).Int()
	if err == nil && n < 0 {
		err = service.ErrVideoTaskNotFound
	}
	return n == 1, err
}

var videoDeleteScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
if cjson.decode(raw).status == 'processing' then return 0 end
redis.call('DEL', KEYS[1])
for i = 2, 5 do redis.call('ZREM', KEYS[i], ARGV[1]) end
redis.call('ZADD', KEYS[6], ARGV[2], ARGV[1])
return 1
`)

func (s *videoWorkbenchStore) Delete(ctx context.Context, task *service.VideoWorkbenchRecord, now time.Time) error {
	n, err := videoDeleteScript.Run(ctx, s.rdb, videoKeys(task), task.ID, now.Unix()).Int()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrVideoTaskProcessing
	}
	if n < 0 {
		return service.ErrVideoTaskNotFound
	}
	return nil
}

func (s *videoWorkbenchStore) ExpiredLeases(ctx context.Context, now time.Time) ([]*service.VideoWorkbenchRecord, error) {
	ids, err := s.rdb.ZRangeByScore(ctx, videoLeases, &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Count: 100}).Result()
	if err != nil {
		return nil, err
	}
	records, err := s.records(ctx, ids)
	if err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(records))
	for _, r := range records {
		live[r.ID] = true
	}
	for _, id := range ids {
		if !live[id] {
			if err := s.rdb.ZRem(ctx, videoLeases, id).Err(); err != nil {
				return nil, err
			}
		}
	}
	return records, nil
}

func (s *videoWorkbenchStore) ExpiredFiles(ctx context.Context, now time.Time) ([]string, error) {
	return s.rdb.ZRangeByScore(ctx, videoCleanup, &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Count: 100}).Result()
}

func (s *videoWorkbenchStore) AcknowledgeCleanup(ctx context.Context, id string) error {
	return s.rdb.ZRem(ctx, videoCleanup, id).Err()
}
