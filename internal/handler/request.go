package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"clientportal/internal/model"
)

func positiveInt64(name, raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", model.ErrValidation, name)
	}
	return n, nil
}

// PathID parses the ":id" path parameter.
func PathID(c *gin.Context) (int64, error) {
	return positiveInt64("id", c.Param("id"))
}

// QueryInt64 parses an optional positive integer query parameter; absent means nil.
func QueryInt64(c *gin.Context, name string) (*int64, error) {
	raw, ok := c.GetQuery(name)
	if !ok {
		return nil, nil
	}
	n, err := positiveInt64(name, raw)
	if err != nil {
		return nil, err
	}
	return &n, nil
}
