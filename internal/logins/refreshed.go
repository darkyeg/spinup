package logins

import (
	"encoding/json"
	"strconv"
	"time"
)

// refreshFields say when a login was last refreshed, most precise first; providers differ.
var refreshFields = []string{"last_refresh", "last_refreshed", "updated_at", "expired", "expire", "expires_at", "expiry"}

var timeLayouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

// refreshedAt is zero when the login carries no recognizable time.
func refreshedAt(data []byte) time.Time {
	var fields map[string]any
	if json.Unmarshal(data, &fields) != nil {
		return time.Time{}
	}
	for _, name := range refreshFields {
		if t := parseTime(fields[name]); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseTime(v any) time.Time {
	switch x := v.(type) {
	case string:
		for _, layout := range timeLayouts {
			if t, err := time.Parse(layout, x); err == nil {
				return t
			}
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return unixTime(n)
		}
	case float64:
		return unixTime(int64(x))
	}
	return time.Time{}
}

// unixTime accepts seconds or milliseconds.
func unixTime(n int64) time.Time {
	switch {
	case n > 1e12:
		return time.UnixMilli(n)
	case n > 0:
		return time.Unix(n, 0)
	}
	return time.Time{}
}

// Summary describes a login without its tokens.
type Summary struct {
	Name      string    `json:"name"`
	Type      string    `json:"type,omitempty"`
	Refreshed time.Time `json:"refreshed"`
}

func Summarize(dir string) []Summary {
	files, _ := Read(dir)
	out := make([]Summary, 0, len(files))
	for _, f := range files {
		var fields struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(f.Data, &fields)
		out = append(out, Summary{Name: f.Name, Type: fields.Type, Refreshed: refreshedAt(f.Data)})
	}
	return out
}
