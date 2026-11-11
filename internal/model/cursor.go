package model

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const cursorDateLayout = "2006-01-02"

// DocumentCursor marks the last row of a page in (document_date DESC, id DESC) order.
type DocumentCursor struct {
	Date time.Time
	ID   int64
}

// Encode returns the opaque string handed to clients.
func (c DocumentCursor) Encode() string {
	raw := c.Date.Format(cursorDateLayout) + "|" + strconv.FormatInt(c.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a string produced by Encode.
func DecodeCursor(s string) (DocumentCursor, error) {
	invalid := fmt.Errorf("%w: invalid cursor", ErrValidation)
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(raw) == 0 {
		return DocumentCursor{}, invalid
	}
	datePart, idPart, ok := strings.Cut(string(raw), "|")
	if !ok {
		return DocumentCursor{}, invalid
	}
	date, err := time.Parse(cursorDateLayout, datePart)
	if err != nil {
		return DocumentCursor{}, invalid
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return DocumentCursor{}, invalid
	}
	return DocumentCursor{Date: date, ID: id}, nil
}
