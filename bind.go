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

func (s *Service) Query(db gas.DatabaseProvider, ctx context.Context, query string, args ...any) (gas.Rows, error) {
	return db.Query(ctx, rebind(db.Driver(), query), args...)
}

func (s *Service) Exec(db gas.DatabaseProvider, ctx context.Context, query string, args ...any) (gas.Result, error) {
	return db.Exec(ctx, rebind(db.Driver(), query), args...)
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
