package dto

import (
	"fmt"
	"strconv"

	"clientportal/internal/model"
)

// Page is one page of a list. NextCursor is absent on the last page.
type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ParseLimit reads the "limit" query value: empty means 20, otherwise 1 to 100.
func ParseLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		return 0, fmt.Errorf("%w: limit must be between 1 and 100", model.ErrValidation)
	}
	return n, nil
}

// MapSlice converts every element and never returns nil, so empty lists encode as [].
func MapSlice[A, B any](in []A, f func(A) B) []B {
	out := make([]B, 0, len(in))
	for _, a := range in {
		out = append(out, f(a))
	}
	return out
}
