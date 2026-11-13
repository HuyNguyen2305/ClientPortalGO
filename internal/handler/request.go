package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

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

// maxBodyBytes caps a request body; the largest real one is a 100-item document.
const maxBodyBytes = 1 << 20

// bindJSON binds the body and reports any failure as a validation error.
// shortcut: an oversized body is a 400, not a 413; change it when a client needs the distinction.
func bindJSON(c *gin.Context, dst any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	if err := c.ShouldBindJSON(dst); err != nil {
		return fmt.Errorf("%w: %s", model.ErrValidation, bindMessage(err))
	}
	return nil
}

// bindMessage describes the first failed rule without the validator's internal struct path.
func bindMessage(err error) string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) && len(ve) > 0 {
		return fmt.Sprintf("%s failed the %q rule", ve[0].Field(), ve[0].Tag())
	}
	return "body is not valid JSON of the expected shape"
}
