-- Opens a new fixed window (start = now, used = amount) when no window is currently live,
-- otherwise adds amount to the live window's used counter. Runs as a single script so the
-- read-decide-write sequence is atomic under concurrent charges for the same token.
--
-- KEYS[1] = window hash key
-- ARGV[1] = now (unix seconds)
-- ARGV[2] = window length in seconds
-- ARGV[3] = amount to add (> 0)

local key = KEYS[1]
local now = tonumber(ARGV[1])
local seconds = tonumber(ARGV[2])
local amount = tonumber(ARGV[3])

local start = redis.call('HGET', key, 'start')
if start == false or (tonumber(start) + seconds) <= now then
    start = now
    redis.call('HSET', key, 'start', start)
    redis.call('HSET', key, 'used', amount)
else
    start = tonumber(start)
    redis.call('HINCRBY', key, 'used', amount)
end

local ttl = (start + seconds) - now
if ttl < 1 then
    ttl = 1
end
redis.call('EXPIRE', key, ttl)
return 1
