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

// The durable cleanup indexes already hold every retained workbench task scored
// by its expiry, so administrators can enumerate results without SCAN.

const workbenchAdminBatchSize = 500

var (
	_ service.ImageWorkbenchAdminStore = (*imageTaskStore)(nil)
	_ service.VideoWorkbenchAdminStore = (*videoWorkbenchStore)(nil)
)

func liveWorkbenchMembers(ctx context.Context, rdb *redis.Client, index string, now time.Time, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	return rdb.ZRevRangeByScore(ctx, index, &redis.ZRangeBy{
		Min: "(" + strconv.FormatInt(now.Unix(), 10), Max: "+inf", Offset: 0, Count: int64(limit),
	}).Result()
}

func (s *imageTaskStore) ListAllWorkbench(ctx context.Context, now time.Time, limit int) ([]*service.ImageTaskRecord, error) {
	members, err := liveWorkbenchMembers(ctx, s.rdb, workbenchCleanupIndex, now, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	seen := make(map[string]bool, len(members))
	for _, member := range members {
		var entry service.ImageWorkbenchCleanup
		if json.Unmarshal([]byte(member), &entry) != nil || !service.ValidImageWorkbenchTaskID(entry.ID) || seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		ids = append(ids, entry.ID)
	}
	tasks := make([]*service.ImageTaskRecord, 0, len(ids))
	for start := 0; start < len(ids); start += workbenchAdminBatchSize {
		end := min(start+workbenchAdminBatchSize, len(ids))
		keys := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			keys = append(keys, imageTaskKey(id))
		}
		values, err := s.rdb.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			raw, ok := value.(string)
			if !ok {
				continue
			}
			var task service.ImageTaskRecord
			// One malformed record must not hide every other user's result.
			if json.Unmarshal([]byte(raw), &task) != nil || task.Workbench == nil {
				continue
			}
			tasks = append(tasks, &task)
		}
	}
	return tasks, nil
}

func (s *videoWorkbenchStore) ListAll(ctx context.Context, now time.Time, limit int) ([]*service.VideoWorkbenchRecord, error) {
	members, err := liveWorkbenchMembers(ctx, s.rdb, videoCleanup, now, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, id := range members {
		if service.ValidVideoWorkbenchTaskID(id) {
			ids = append(ids, id)
		}
	}
	tasks := make([]*service.VideoWorkbenchRecord, 0, len(ids))
	for start := 0; start < len(ids); start += workbenchAdminBatchSize {
		end := min(start+workbenchAdminBatchSize, len(ids))
		keys := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			keys = append(keys, videoTaskPrefix+id)
		}
		values, err := s.rdb.MGet(ctx, keys...).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}
		for _, value := range values {
			raw, ok := value.(string)
			if !ok {
				continue
			}
			var task service.VideoWorkbenchRecord
			if json.Unmarshal([]byte(raw), &task) != nil {
				continue
			}
			tasks = append(tasks, &task)
		}
	}
	return tasks, nil
}
