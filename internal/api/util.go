package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}

// pathParam 从 URL 路径中提取 prefix 和 suffix 之间的部分
func pathParam(path, prefix, suffix string) string {
	p := strings.TrimPrefix(path, prefix)
	if suffix != "" {
		idx := strings.Index(p, suffix)
		if idx < 0 {
			return ""
		}
		return p[:idx]
	}
	return p
}
