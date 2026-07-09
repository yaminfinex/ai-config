// Package sqlitedsn centralizes SQLite DSN construction for the sesh store.
package sqlitedsn

import (
	"net/url"
	"path/filepath"
)

// ReadWrite returns a SQLite URI that applies the busy timeout to every
// connection opened by the driver.
func ReadWrite(path string) string {
	return withOptions(path, nil)
}

// ReadOnly returns a read-only SQLite URI with the same busy timeout posture
// as store writers.
func ReadOnly(path string) string {
	return withOptions(path, url.Values{"mode": []string{"ro"}})
}

func withOptions(path string, values url.Values) string {
	if values == nil {
		values = url.Values{}
	}
	values.Add("_pragma", "busy_timeout(5000)")
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	u := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(path),
	}
	u.RawQuery = values.Encode()
	return u.String()
}
