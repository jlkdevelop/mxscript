// redis.go — thin wrapper around go-redis/redis/v9. Exposed as the
// `redis` namespace:
//
//	let r = redis.connect("redis://localhost:6379/0")
//	redis.set(r, "user:1", "Jassim", { ttl_seconds: 3600 })
//	let name = redis.get(r, "user:1")
//	redis.del(r, "user:1")
//	redis.publish(r, "events", json_stringify({ kind: "ping" }))
//
// Connection objects are dbHandle-style opaque handles. Pipelining and
// streams aren't surfaced yet; callers who need them can drop into Go
// or open an issue.
package interpreter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisHandle struct {
	client *redis.Client
}

func redisConnect(url string) (*redisHandle, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &redisHandle{client: c}, nil
}

func mustRedisHandle(args []Value) (*redisHandle, error) {
	if len(args) < 1 || args[0].Kind != KindHandle {
		return nil, fmt.Errorf("expected a redis.connect handle as first argument")
	}
	h, ok := args[0].Handle.(*redisHandle)
	if !ok {
		return nil, fmt.Errorf("argument is not a redis handle")
	}
	return h, nil
}

func builtinRedisConnect(i *Interpreter, args []Value) (Value, error) {
	url, err := stringArg(args, 0)
	if err != nil {
		return Value{}, err
	}
	h, err := redisConnect(url)
	if err != nil {
		return Value{}, err
	}
	return HandleValue(h), nil
}

func builtinRedisSet(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 {
		return Value{}, fmt.Errorf("redis.set(r, key, value, opts?) requires a value")
	}
	val := args[2].Display()
	ttl := time.Duration(0)
	if len(args) > 3 && args[3].Kind == KindObject {
		if v, ok := args[3].Object.Get("ttl_seconds"); ok && v.Kind == KindNumber {
			ttl = time.Duration(v.Number) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.client.Set(ctx, key, val, ttl).Err(); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

func builtinRedisGet(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v, err := h.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return NullValue(), nil
	}
	if err != nil {
		return Value{}, err
	}
	return StringValue(v), nil
}

func builtinRedisDel(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	keys := make([]string, 0, len(args)-1)
	for _, a := range args[1:] {
		if a.Kind != KindString {
			return Value{}, fmt.Errorf("redis.del expects string keys")
		}
		keys = append(keys, a.String)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := h.client.Del(ctx, keys...).Result()
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(n)), nil
}

func builtinRedisIncr(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := h.client.Incr(ctx, key).Result()
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(n)), nil
}

func builtinRedisExpire(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	key, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	secs, err := numberArg(args, 2)
	if err != nil {
		return Value{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.client.Expire(ctx, key, time.Duration(secs)*time.Second).Err(); err != nil {
		return Value{}, err
	}
	return NullValue(), nil
}

func builtinRedisPublish(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	channel, err := stringArg(args, 1)
	if err != nil {
		return Value{}, err
	}
	if len(args) < 3 {
		return Value{}, fmt.Errorf("redis.publish(r, channel, message) requires a message")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n, err := h.client.Publish(ctx, channel, args[2].Display()).Result()
	if err != nil {
		return Value{}, err
	}
	return NumberValue(float64(n)), nil
}

func builtinRedisClose(i *Interpreter, args []Value) (Value, error) {
	h, err := mustRedisHandle(args)
	if err != nil {
		return Value{}, err
	}
	return NullValue(), h.client.Close()
}
