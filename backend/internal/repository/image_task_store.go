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
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if task.Workbench == nil {
		return s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
	}
	cleanup, err := json.Marshal(service.ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail})
	if err != nil {
		return err
	}
	_, err = s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, imageTaskKey(task.ID), data, ttl)
		pipe.ZAdd(ctx, workbenchTaskIndex(task.UserID), redis.Z{Score: float64(task.ExpiresAt), Member: task.ID})
		pipe.Expire(ctx, workbenchTaskIndex(task.UserID), max(24*time.Hour, ttl))
		if task.Status != service.ImageTaskStatusProcessing {
			pipe.ZRem(ctx, workbenchActiveIndex(task.UserID), task.ID)
			pipe.ZAdd(ctx, workbenchCleanupIndex, redis.Z{Score: float64(task.ExpiresAt), Member: string(cleanup)})
		}
		return nil
	})
	return err
}

const workbenchCleanupIndex = "image_workbench:cleanup"

func workbenchTaskIndex(userID int64) string {
	return "image_workbench:tasks:" + strconv.FormatInt(userID, 10)
}

func workbenchActiveIndex(userID int64) string {
	return "image_workbench:active:" + strconv.FormatInt(userID, 10)
}

var createWorkbenchScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
if tonumber(ARGV[2]) > 0 and redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[2]) then
  return 0
end
redis.call('SET', KEYS[3], ARGV[5], 'PX', ARGV[6])
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[4])
redis.call('ZADD', KEYS[2], ARGV[3], ARGV[4])
redis.call('ZADD', KEYS[4], ARGV[3], ARGV[7])
redis.call('PEXPIRE', KEYS[1], math.max(86400000, tonumber(ARGV[6])))
redis.call('PEXPIRE', KEYS[2], math.max(86400000, tonumber(ARGV[6])))
return 1
`)

func (s *imageTaskStore) CreateWorkbench(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration, limit int) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	cleanup, err := json.Marshal(service.ImageWorkbenchCleanup{ID: task.ID, UserEmail: task.UserEmail})
	if err != nil {
		return err
	}
	created, err := createWorkbenchScript.Run(ctx, s.rdb,
		[]string{workbenchActiveIndex(task.UserID), workbenchTaskIndex(task.UserID), imageTaskKey(task.ID), workbenchCleanupIndex},
		time.Now().Unix(), limit, task.ExpiresAt, task.ID, data, ttl.Milliseconds(), string(cleanup)).Int()
	if err != nil {
		return err
	}
	if created == 0 {
		return service.ErrImageWorkbenchLimit
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
	tasks := make([]*service.ImageTaskRecord, 0, len(ids))
	for _, id := range ids {
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

func (s *imageTaskStore) DeleteWorkbench(ctx context.Context, task *service.ImageTaskRecord) error {
	_, err := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, imageTaskKey(task.ID))
		pipe.ZRem(ctx, workbenchTaskIndex(task.UserID), task.ID)
		pipe.ZRem(ctx, workbenchActiveIndex(task.UserID), task.ID)
		return nil
	})
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
