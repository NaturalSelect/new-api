-- Restores one persisted window snapshot after a Redis/memory loss. Returns:
--   1 = seeded (the persisted window was adopted, or merged into a newer live window)
--   2 = a live window already covers this one; left untouched to avoid clobbering fresher data
--   0 = the persisted window itself has already ended
--
-- KEYS[1] = window hash key
-- ARGV[1] = now (unix seconds)
-- ARGV[2] = window length in seconds
-- ARGV[3] = persisted window start
-- ARGV[4] = persisted used

local key = KEYS[1]
local now = tonumber(ARGV[1])
local seconds = tonumber(ARGV[2])
local pstart = tonumber(ARGV[3])
local pused = tonumber(ARGV[4])

if pstart + seconds <= now then
    return 0
end

local newStart = pstart
local newUsed = pused

local start = redis.call('HGET', key, 'start')
if start ~= false and (tonumber(start) + seconds) > now then
    start = tonumber(start)
    if start <= pstart then
        -- The live window already is (or supersedes) the persisted one: it is the
        -- authoritative source, so the DB snapshot must not overwrite it.
        return 2
    end
    -- The live window opened after the persisted window's start, meaning the persisted
    -- window had not actually ended yet when data was lost: fold the live usage that
    -- accumulated since into the restored window instead of discarding either side.
    local used = tonumber(redis.call('HGET', key, 'used'))
    newUsed = pused + used
end

redis.call('HSET', key, 'start', newStart)
redis.call('HSET', key, 'used', newUsed)
local ttl = (newStart + seconds) - now
if ttl < 1 then
    ttl = 1
end
redis.call('EXPIRE', key, ttl)
return 1
