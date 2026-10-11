package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"

type imageTaskStore struct {
	rdb *redis.Client
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if task.Workbench != nil {
		return s.saveWorkbench(ctx, task, ttl, 0)
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
}

var saveWorkbenchScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local current = cjson.decode(raw)
if tonumber(ARGV[7]) > 0 and (current.status ~= 'queued' or
  (tonumber(current.queue_lease_until or 0) > tonumber(ARGV[7]) and tonumber(current.queue_deadline or 0) > tonumber(ARGV[7]))) then
  return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('PEXPIRE', KEYS[2], math.max(86400000, tonumber(ARGV[2])))
if ARGV[5] ~= 'processing' then redis.call('ZREM', KEYS[3], ARGV[4]) end
if ARGV[5] ~= 'queued' then
  redis.call('ZREM', KEYS[5], ARGV[4])
  redis.call('ZREM', KEYS[6], ARGV[4])
end
redis.call('ZADD', KEYS[4], ARGV[3], ARGV[6])
return 1
`)

func (s *imageTaskStore) saveWorkbench(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration, expiredBefore int64) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	cleanup, err := json.Marshal(service.ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail, Storage: task.WorkbenchStorage})
	if err != nil {
		return err
	}
	result, err := saveWorkbenchScript.Run(ctx, s.rdb,
		[]string{imageTaskKey(task.ID), workbenchTaskIndex(task.UserID), workbenchActiveIndex(task.UserID), workbenchCleanupIndex, workbenchQueueIndex(task.UserID), workbenchQueueLeases},
		data, ttl.Milliseconds(), task.ExpiresAt, task.ID, task.Status, string(cleanup), expiredBefore).Int()
	if err == nil && result == -1 {
		return service.ErrImageTaskNotFound
	}
	return err
}

const workbenchCleanupIndex = "image_workbench:cleanup"
const workbenchQueueLeases = "image_workbench:queue_leases"

func workbenchTaskIndex(userID int64) string {
	return "image_workbench:tasks:" + strconv.FormatInt(userID, 10)
}

func workbenchActiveIndex(userID int64) string {
	return "image_workbench:active:" + strconv.FormatInt(userID, 10)
}

func workbenchQueueIndex(userID int64) string {
	return "image_workbench:queued:" + strconv.FormatInt(userID, 10)
}

var createWorkbenchScript = redis.NewScript(`
for _, id in ipairs(redis.call('ZRANGE', KEYS[1], 0, -1)) do
  local raw = redis.call('GET', ARGV[9] .. id)
  local pending = raw and cjson.decode(raw)
  if not pending or pending.status ~= 'queued' or tonumber(pending.queue_lease_until or 0) * 1000 <= tonumber(ARGV[1]) then
    redis.call('ZREM', KEYS[1], id)
  end
end
if tonumber(ARGV[2]) > 0 and redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then
  return 0
end
redis.call('SET', KEYS[3], ARGV[5], 'PX', ARGV[6])
redis.call('ZADD', KEYS[1], ARGV[1], ARGV[4])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('ZADD', KEYS[4], ARGV[3], ARGV[7])
redis.call('ZADD', KEYS[5], ARGV[8], ARGV[4])
redis.call('PEXPIRE', KEYS[1], math.max(86400000, tonumber(ARGV[6])))
redis.call('PEXPIRE', KEYS[2], math.max(86400000, tonumber(ARGV[6])))
return 1
`)

func (s *imageTaskStore) CreateWorkbench(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration, limit int) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	cleanup, err := json.Marshal(service.ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail, Storage: task.WorkbenchStorage})
	if err != nil {
		return err
	}
	created, err := createWorkbenchScript.Run(ctx, s.rdb,
		[]string{workbenchQueueIndex(task.UserID), workbenchTaskIndex(task.UserID), imageTaskKey(task.ID), workbenchCleanupIndex, workbenchQueueLeases},
		time.Now().UnixMilli(), limit, task.ExpiresAt, task.ID, data, ttl.Milliseconds(), string(cleanup), task.QueueLeaseUntil, imageTaskKeyPrefix).Int()
	if err != nil {
		return err
	}
	if created == 0 {
		return service.ErrImageWorkbenchQueueFull
	}
	return nil
}

func (s *imageTaskStore) ListWorkbench(ctx context.Context, userID int64, now time.Time) ([]*service.ImageTaskRecord, error) {
	index := workbenchTaskIndex(userID)
	if err := s.rdb.ZRemRangeByScore(ctx, index, "-inf", strconv.FormatInt(now.Unix(), 10)).Err(); err != nil {
		return nil, err
	}
	ids, err := s.rdb.ZRevRange(ctx, index, 0, 199).Result()
	if err != nil {
		return nil, err
	}
	// Retained results must never push accepted, unfinished tasks out of the list.
	for _, pendingIndex := range []string{workbenchQueueIndex(userID), workbenchActiveIndex(userID)} {
		pending, err := s.rdb.ZRange(ctx, pendingIndex, 0, -1).Result()
		if err != nil {
			return nil, err
		}
		ids = append(ids, pending...)
	}
	tasks := make([]*service.ImageTaskRecord, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		task, err := s.Get(ctx, id)
		if err == service.ErrImageTaskNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		if task.UserID == userID && task.Workbench != nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (s *imageTaskStore) ExpiredWorkbench(ctx context.Context, now time.Time) ([]service.ImageWorkbenchCleanup, error) {
	members, err := s.rdb.ZRangeByScore(ctx, workbenchCleanupIndex, &redis.ZRangeBy{
		Min: "-inf", Max: strconv.FormatInt(now.Unix(), 10), Offset: 0, Count: 100,
	}).Result()
	if err != nil {
		return nil, err
	}
	items := make([]service.ImageWorkbenchCleanup, 0, len(members))
	for _, member := range members {
		var item service.ImageWorkbenchCleanup
		if err := json.Unmarshal([]byte(member), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *imageTaskStore) RemoveWorkbenchCleanup(ctx context.Context, task service.ImageWorkbenchCleanup) error {
	member, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.ZRem(ctx, workbenchCleanupIndex, string(member)).Err()
}

var deleteWorkbenchScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then return -1 end
local task = cjson.decode(raw)
if task.user_id ~= tonumber(ARGV[2]) then return -1 end
if task.status == 'processing' then return 0 end
redis.call('DEL', KEYS[1])
for i = 2, 5 do redis.call('ZREM', KEYS[i], ARGV[1]) end
return 1
`)

func (s *imageTaskStore) DeleteWorkbench(ctx context.Context, task *service.ImageTaskRecord) error {
	result, err := deleteWorkbenchScript.Run(ctx, s.rdb,
		[]string{imageTaskKey(task.ID), workbenchTaskIndex(task.UserID), workbenchActiveIndex(task.UserID), workbenchQueueIndex(task.UserID), workbenchQueueLeases}, task.ID, task.UserID).Int()
	if err == nil {
		switch result {
		case -1:
			return service.ErrImageTaskNotFound
		case 0:
			return service.ErrImageTaskProcessing
		}
	}
	return err
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}
