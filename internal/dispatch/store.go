package dispatch

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/rueidis"

	"github.com/Aset-Mk/geomatch/internal/index"
)

// driverKey is absent while the driver is free, otherwise it holds "offer:<order>" with a TTL or "ride:<order>" without one
// The {city} tag puts all drivers of the city in one cluster slot, so one script can take any of them
func driverKey(id index.DriverID) string {
	return "{city}:driver:" + strconv.FormatUint(uint64(id), 10)
}

func offerValue(order uint64) string { return "offer:" + strconv.FormatUint(order, 10) }
func rideValue(order uint64) string  { return "ride:" + strconv.FormatUint(order, 10) }

// offerScript takes the first free candidate in one round trip, the candidates come in KEYS by distance
var offerScript = rueidis.NewLuaScript(`
for i, key in ipairs(KEYS) do
  if redis.call('SET', key, ARGV[1], 'NX', 'PX', ARGV[2]) then
    return i
  end
end
return 0
`)

// moveScript changes the value of KEYS[1] from ARGV[1] to ARGV[2] and deletes the key when ARGV[2] is empty
var moveScript = rueidis.NewLuaScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return 0
end
if ARGV[2] == '' then
  redis.call('DEL', KEYS[1])
else
  redis.call('SET', KEYS[1], ARGV[2])
end
return 1
`)

// Store keeps driver states in Redis
type Store struct {
	c rueidis.Client
}

func NewStore(c rueidis.Client) *Store {
	return &Store{c: c}
}

// Offer marks the first free candidate as offered the order for ttl and returns that driver
func (s *Store) Offer(ctx context.Context, order uint64, cands []index.DriverID, ttl time.Duration) (index.DriverID, bool, error) {
	val, ms := offerValue(order), strconv.FormatInt(ttl.Milliseconds(), 10)
	keys := make([]string, len(cands))
	for i, id := range cands {
		keys[i] = driverKey(id)
	}
	i, err := offerScript.Exec(ctx, s.c, keys, []string{val, ms}).AsInt64()
	if err != nil || i == 0 {
		return 0, false, err
	}
	return cands[i-1], true, nil
}

// Accept turns the open offer into a ride, false when the offer is gone
func (s *Store) Accept(ctx context.Context, order uint64, driver index.DriverID) (bool, error) {
	return s.move(ctx, driver, offerValue(order), rideValue(order))
}

// Release frees a driver who declined or did not answer, false when the offer is gone
func (s *Store) Release(ctx context.Context, order uint64, driver index.DriverID) (bool, error) {
	return s.move(ctx, driver, offerValue(order), "")
}

// Complete frees a driver after the ride, false when the driver is not on this order
func (s *Store) Complete(ctx context.Context, order uint64, driver index.DriverID) (bool, error) {
	return s.move(ctx, driver, rideValue(order), "")
}

func (s *Store) move(ctx context.Context, driver index.DriverID, from, to string) (bool, error) {
	n, err := moveScript.Exec(ctx, s.c, []string{driverKey(driver)}, []string{from, to}).AsInt64()
	return n == 1, err
}
