// Package metrics keeps the few in-process counters exported on /metrics.
package metrics

import (
	"sort"
	"sync"
)

var (
	mu     sync.Mutex
	logins = map[[2]string]uint64{} // {channel, result} -> count
)

// Login counts a sign-in attempt. channel: "web", "client", "passkey" or "sso".
func Login(channel string, ok bool) {
	result := "failure"
	if ok {
		result = "success"
	}
	mu.Lock()
	logins[[2]string{channel, result}]++
	mu.Unlock()
}

type LoginCount struct {
	Channel, Result string
	Count           uint64
}

// Logins returns the login counters in a stable order.
func Logins() []LoginCount {
	mu.Lock()
	defer mu.Unlock()
	out := make([]LoginCount, 0, len(logins))
	for k, v := range logins {
		out = append(out, LoginCount{k[0], k[1], v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].Result < out[j].Result
	})
	return out
}
