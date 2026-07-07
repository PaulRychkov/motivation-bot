package models

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"
)

type Date struct {
	time.Time
}

const dateLayout = "2006-01-02"

func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return NewDate(y, m, d)
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return Date{t}, nil
}

func (d Date) String() string {
	return d.Format(dateLayout)
}

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.Format(dateLayout) + `"`), nil
}

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" || s == "" {
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Date) Value() (driver.Value, error) {
	return d.Format(dateLayout), nil
}

func (d *Date) Scan(value any) error {
	switch v := value.(type) {
	case time.Time:
		*d = DateOf(v)
		return nil
	case string:
		if len(v) < len(dateLayout) {
			return fmt.Errorf("scan date: слишком короткое значение %q", v)
		}
		parsed, err := ParseDate(v[:len(dateLayout)])
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		return d.Scan(string(v))
	case nil:
		return nil
	default:
		return fmt.Errorf("scan date: unsupported type %T", value)
	}
}
