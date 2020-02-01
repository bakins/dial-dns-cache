package dial

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDial(t *testing.T) {
	tests := []struct {
		name            string
		host            string
		addrs           []string
		resolveError    string
		expectedError   string
		expectedAddress string
	}{
		{
			name:          noSuchHost,
			host:          "testing123",
			resolveError:  noSuchHost,
			expectedError: noSuchHost,
		},
		{
			name:          "empty addrs",
			host:          "testing123",
			expectedError: noSuchHost,
		},
		{
			name:            "single address",
			host:            "testing123",
			addrs:           []string{"127.0.0.1"},
			expectedAddress: "127.0.0.1",
		},
		{
			name:            "multiple address",
			host:            "testing123",
			addrs:           []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4"},
			expectedAddress: "127.0.0.3",
		},
		{
			name:            "ip",
			host:            "127.0.0.1",
			expectedAddress: "127.0.0.1",
		},
	}

	// set seed so we get same addresses
	rand.Seed(0)

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			r := &testResolver{
				addrs: test.addrs,
			}

			if test.resolveError != "" {
				r.err = errors.New(test.resolveError)
			}

			c := New(
				WithTTL(time.Hour),
				WithDialer(&testDialer{}),
				WithResolver(r),
			)

			// run multiple times to ensure we use cache
			for i := 0; i < 2; i++ {
				conn, err := c.Dial("tcp", test.host)

				if test.expectedError != "" {
					require.Error(t, err)
					require.Contains(t, err.Error(), test.expectedError)
					continue
				}

				require.NoError(t, err)

				tc, ok := conn.(*testConn)
				require.True(t, ok)

				require.Equal(t, test.expectedAddress, tc.address)
			}
		})
	}
}

type testResolver struct {
	err   error
	addrs []string
}

func (t *testResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return t.addrs, t.err
}

type testDialer struct {
}

func (t *testDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return &testConn{address: address}, nil
}

type testConn struct {
	address string
	net.IPConn
}
