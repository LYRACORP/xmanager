package ops

import (
	"encoding/json"
	"strconv"
)

func argUint(args map[string]any, key string) uint {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return uint(x)
	case json.Number:
		n, _ := x.Int64()
		return uint(n)
	case string:
		n, _ := strconv.ParseUint(x, 10, 64)
		return uint(n)
	case int:
		return uint(x)
	case int64:
		return uint(x)
	case uint:
		return x
	}
	return 0
}

func argStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func argBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1" || x == "yes"
	}
	return false
}

func argUintSlice(args map[string]any, key string) []uint {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]uint, 0, len(arr))
	for _, item := range arr {
		out = append(out, argUint(map[string]any{"x": item}, "x"))
	}
	return out
}

func argInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return def
}
