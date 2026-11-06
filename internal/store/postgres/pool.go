package postgres

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pingTimeout = 5 * time.Second

// newPool opens a connection pool and confirms the database is reachable.
// Errors are never derived from the driver's raw error text, since parse
// failures can echo the offending input (including credentials).
func newPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, errors.New("database url is required")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("failed to parse database url")
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("failed to open database connection pool")
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, unreachable(err, config.ConnConfig.Host, config.ConnConfig.Port, config.ConnConfig.Database)
	}

	return pool, nil
}

// unreachable says why a connection failed, in fixed words chosen from the
// error's type and never its text (the driver's text can echo the connection
// string, credentials included). The only values it adds are the host, port and
// database name the operator supplied, which are not secret. Without this every
// cause read the same, and an operator on a container host with no shell to test
// from could not tell an unresolvable name from a refused connection or a wrong
// password. It also records whether waiting could help (see IsTransient).
func unreachable(err error, host string, port uint16, database string) error {
	problem, transient := connectionProblem(err, host, net.JoinHostPort(host, strconv.Itoa(int(port))), database)
	if problem == "" {
		return &connectError{message: "database is unreachable", transient: true}
	}
	return &connectError{message: "database is unreachable: " + problem, transient: transient}
}

func connectionProblem(err error, host, address, database string) (string, bool) {
	var pgErr *pgconn.PgError
	var dnsErr *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &pgErr):
		switch pgErr.Code {
		case "28P01", "28000":
			return "PostgreSQL rejected the user name or password", false
		case "3D000":
			return fmt.Sprintf("the database %q does not exist on the server", database), false
		case "57P03":
			return "PostgreSQL is still starting up and is not accepting connections yet", true
		case "53300":
			return "PostgreSQL has no free connections", true
		}
		return "PostgreSQL refused the connection (SQLSTATE " + pgErr.Code + ")", false
	case errors.As(err, &dnsErr):
		return fmt.Sprintf("the host name %q could not be resolved (a container name only resolves on a user-defined Docker network)", host), true
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Sprintf("nothing accepted the connection at %s (is PostgreSQL running, and is the port right?)", address), true
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return fmt.Sprintf("timed out connecting to %s (check that the host name resolves and that the address is reachable from this container)", address), true
	case strings.Contains(strings.ToLower(err.Error()), "tls"):
		return fmt.Sprintf("a secure (TLS) connection to %s could not be negotiated (check the SSL mode setting)", address), false
	}
	return "", true
}
