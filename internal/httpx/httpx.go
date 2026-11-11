// Package httpx holds small JSON and paging helpers shared by the API handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// MaxBody limits request bodies on JSON endpoints.
const MaxBody = 1 << 20

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes {"error": msg}, the shape both the RustDesk client and our UI expect.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// Decode reads a JSON body into v. Numbers are kept as json.Number when v is a map.
func Decode(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	if err != nil {
		return err
	}
	if len(body) > MaxBody {
		return errors.New("request body too large")
	}
	if len(body) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(body, v)
}

type Page struct {
	Current  int
	PageSize int
}

func (p Page) Offset() int { return (p.Current - 1) * p.PageSize }

// ParsePage reads ?current=&pageSize= (the client's paging names), with sane bounds.
func ParsePage(r *http.Request) Page {
	q := r.URL.Query()
	cur, _ := strconv.Atoi(q.Get("current"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if cur < 1 {
		cur = 1
	}
	if size < 1 || size > 500 {
		size = 50
	}
	return Page{Current: cur, PageSize: size}
}

// Paged is the {total, data} envelope used by the client's list endpoints.
type Paged[T any] struct {
	Total int64 `json:"total"`
	Data  []T   `json:"data"`
}
