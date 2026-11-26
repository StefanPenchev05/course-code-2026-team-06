package redis

import (
	"context"
	"time"

	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/redis/go-redis/v9"
)

const watchPrefix = "tutti:{realtime}:watches:"

type Watches struct {
	client *redis.Client
	prefix string
}

func NewWatches(client *redis.Client) *Watches { return &Watches{client: client, prefix: watchPrefix} }

var registerWatch = redis.NewScript(`
local t=redis.call('TIME'); local now=t[1]*1000+math.floor(t[2]/1000); local until_at=now+600000
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
redis.call('ZREMRANGEBYSCORE',KEYS[3],'-inf',now)
if not redis.call('ZSCORE',KEYS[1],ARGV[2]) and
   (redis.call('ZCARD',KEYS[1])>=32 or redis.call('ZCARD',KEYS[3])>=16384) then return 0 end
redis.call('ZADD',KEYS[1],until_at,ARGV[2]);redis.call('PEXPIRE',KEYS[1],600000)
redis.call('ZADD',KEYS[2],until_at,ARGV[1]);redis.call('PEXPIRE',KEYS[2],600000)
redis.call('ZADD',KEYS[3],until_at,ARGV[1]..':'..ARGV[2]);redis.call('PEXPIRE',KEYS[3],600000)
return until_at`)

func (w *Watches) Register(ctx context.Context, user, tutor string) (time.Time, error) {
	if !app.ValidID(user) || !app.ValidID(tutor) {
		return time.Time{}, app.ErrWatch
	}
	end, err := registerWatch.Run(ctx, w.client, []string{w.prefix + "user:" + user, w.prefix + "tutor:" + tutor, w.prefix + "all"}, user, tutor).Int64()
	if err != nil {
		return time.Time{}, err
	}
	if end == 0 {
		return time.Time{}, app.ErrWatch
	}
	return time.UnixMilli(end).UTC(), nil
}

var watchUsers = redis.NewScript(`local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now);return redis.call('ZRANGE',KEYS[1],0,-1)`)

func (w *Watches) Users(ctx context.Context, tutor string) ([]string, error) {
	if !app.ValidID(tutor) {
		return nil, app.ErrWatch
	}
	return watchUsers.Run(ctx, w.client, []string{w.prefix + "tutor:" + tutor}).StringSlice()
}

var hasWatch = redis.NewScript(`local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
local score=redis.call('ZSCORE',KEYS[1],ARGV[1]);if score and tonumber(score)>now then return 1 else return 0 end`)

func (w *Watches) Contains(ctx context.Context, user, tutor string) (bool, error) {
	if !app.ValidID(user) || !app.ValidID(tutor) {
		return false, app.ErrWatch
	}
	n, err := hasWatch.Run(ctx, w.client, []string{w.prefix + "user:" + user}, tutor).Int()
	return n == 1, err
}
