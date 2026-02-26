package migrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/gasmod/gas"
)

const (
	bindTypeUnknown = iota
	bindTypeQuestionMark
	bindTypePositional
)

var driverBindingType = map[string]int{
	"postgres": bindTypePositional,
	"pgx":      bindTypePositional,
	"mysql":    bindTypeQuestionMark,
	"sqlite":   bindTypeQuestionMark,
}

func getDriverBindingType(driver string) int {
	bindingType, ok := driverBindingType[driver]
	if !ok {
		return bindTypeUnknown
	}
	return bindingType
}

func rebind(driver, query string) string {
	bindingType := getDriverBindingType(driver)

	// if binding is ? or unknown, return the query as-is
	if bindingType == bindTypeUnknown || bindingType == bindTypeQuestionMark {
		return query
	}

	// rebind to positional parameters
	var b strings.Builder
	n := 1
	for _, c := range query {
		if c == '?' {
			_, _ = fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

//nolint:revive // intentional
func (s *Service) query(db gas.DatabaseProvider, ctx context.Context, query string, args ...any) (gas.Rows, error) {
	//nolint:wrapcheck // intentional
	return db.Query(ctx, rebind(db.Driver(), query), args...)
}

//nolint:revive // intentional
func (s *Service) exec(db gas.DatabaseProvider, ctx context.Context, query string, args ...any) (gas.Result, error) {
	//nolint:wrapcheck // intentional
	return db.Exec(ctx, rebind(db.Driver(), query), args...)
}
