// Package dial provides a DNS caching network Dialer.
package dial

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

type lookupResult struct {
	expires time.Time
	addrs   []string
}

// A Resolver looks up hosts.
type Resolver interface {
	LookupHost(ctx context.Context, host string) (addrs []string, err error)
}

// Dialer dials
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Cache is a dialer that caches dns lookups.
type Cache struct {
	lock    sync.RWMutex
	options Options
	entries map[string]*lookupResult
}

// Options is used to create a new cache dialer.
type Options struct {
	ttl      time.Duration // override ttl
	maxItems int
	resolver Resolver
	dialer   Dialer
}

// OptionFunc is used to set options when creating a new cache dialer
type OptionFunc func(*Options)

// TODO: support using ttl of item.
// look at "github.com/miekg/dns" or a higher level package
var defaultOptions = Options{
	ttl:      time.Second * 10,
	maxItems: 1024,
}

// New returns a new cache
func New(options ...OptionFunc) *Cache {
	c := Cache{
		options: defaultOptions,
	}

	for _, o := range options {
		o(&c.options)
	}

	if c.options.maxItems > 0 {
		c.entries = make(map[string]*lookupResult, c.options.maxItems+1)
	}

	if c.options.resolver == nil {
		c.options.resolver = net.DefaultResolver
	}

	if c.options.dialer == nil {
		c.options.dialer = &net.Dialer{}
	}

	return &c
}

// WithTTL sets the cache ttl. default is 10 seconds.
// Set to 0 to disable cache.
func WithTTL(ttl time.Duration) OptionFunc {
	return func(o *Options) {
		o.ttl = ttl
	}
}

// WithMaxItems sets the number of cache items. default is 1024.
// Set to 0 to disable cache.
func WithMaxItems(max int) OptionFunc {
	return func(o *Options) {
		o.maxItems = max
	}
}

// WithResolver sets the resolver.
// default is net.DefaultResolver.
func WithResolver(r Resolver) OptionFunc {
	return func(o *Options) {
		o.resolver = r
	}
}

// WithDialer sets the parent dials.
// default is net.Dialer{}.
func WithDialer(d Dialer) OptionFunc {
	return func(o *Options) {
		o.dialer = d
	}
}

// can be changed for tests
var nowFunc = time.Now

func (c *Cache) get(name string) ([]string, bool) {
	now := nowFunc()

	c.lock.RLock()
	defer c.lock.RUnlock()

	if r, ok := c.entries[name]; ok && r.expires.After(now) {
		return r.addrs, true
	}

	return nil, false
}

func (c *Cache) set(name string, addrs []string) {
	result := lookupResult{
		addrs:   addrs,
		expires: nowFunc().Add(c.options.ttl),
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	c.entries[name] = &result

	if len(c.entries) > c.options.maxItems {
		// delete random entry
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
}

const noSuchHost = "no such host"

var errNoSuchHost = errors.New(noSuchHost)

func (c *Cache) lookup(ctx context.Context, name string) ([]string, error) {
	if c.options.maxItems == 0 || c.options.ttl == 0 {
		return c.options.resolver.LookupHost(ctx, name)
	}

	addrs, ok := c.get(name)
	if ok {
		if len(addrs) == 0 {
			return nil, errNoSuchHost
		}
		return addrs, nil
	}

	if ip := net.ParseIP(name); ip != nil {
		addrs := []string{name}
		c.set(name, addrs)
		return addrs, nil
	}

	addrs, err := c.options.resolver.LookupHost(ctx, name)
	if err != nil {
		// stdlib net does not expose this, so check with string matching
		if strings.Contains(err.Error(), noSuchHost) {
			c.set(name, nil)
			return nil, errNoSuchHost
		}
	}

	if len(addrs) == 0 {
		c.set(name, nil)
		return nil, errNoSuchHost
	}

	c.set(name, addrs)

	return addrs, nil
}

// DialContext connects to the address on the named network using the provided context.
func (c *Cache) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	addrs, err := c.lookup(ctx, address)
	if err != nil {
		return nil, err
	}

	if len(addrs) == 1 {
		return c.options.dialer.DialContext(ctx, network, addrs[0])
	}

	// since we cache, do our oun randomization or we will use same
	// address for life of cached item
	idx := rand.Intn(len(addrs))
	return c.options.dialer.DialContext(ctx, network, addrs[idx])
}

// Dial connects to the address on the named network.
func (c *Cache) Dial(network, address string) (net.Conn, error) {
	return c.DialContext(context.Background(), network, address)
}
