package ranger

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Range 表示一个 HTTP Range 请求的范围。
type Range struct {
	Start  int64
	End    int64
	Length int64 // 资源总长度
}

// ParseRange 解析 Range 请求头，返回解析后的范围列表。
// 支持格式：bytes=0-499, bytes=500-, bytes=-500
func ParseRange(rangeHeader string, totalLength int64) ([]Range, error) {
	if rangeHeader == "" {
		return nil, nil
	}

	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, fmt.Errorf("invalid range unit: %s", rangeHeader)
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	rangesStr := strings.Split(rangeSpec, ",")

	var ranges []Range
	for _, r := range rangesStr {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		parsed, err := parseSingleRange(r, totalLength)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, parsed)
	}

	if len(ranges) == 0 {
		return nil, fmt.Errorf("no valid range specified")
	}

	return ranges, nil
}

func parseSingleRange(spec string, totalLength int64) (Range, error) {
	dashIdx := strings.Index(spec, "-")
	if dashIdx < 0 {
		return Range{}, fmt.Errorf("invalid range format: %s", spec)
	}

	var startStr, endStr string
	if dashIdx == 0 {
		// 后缀范围：-500
		startStr = "-" + spec[dashIdx+1:]
	} else {
		startStr = spec[:dashIdx]
		endStr = spec[dashIdx+1:]
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return Range{}, fmt.Errorf("invalid range start: %s", startStr)
	}

	var end int64
	if endStr != "" {
		end, err = strconv.ParseInt(endStr, 10, 64)
		if err != nil {
			return Range{}, fmt.Errorf("invalid range end: %s", endStr)
		}
	}

	if start < 0 {
		// 后缀范围：-500 表示最后 500 字节
		start = totalLength + start
		if start < 0 {
			start = 0
		}
		end = totalLength - 1
	} else {
		// 前缀范围：start-
		if endStr == "" {
			end = totalLength - 1
		}
		if end >= totalLength {
			end = totalLength - 1
		}
	}

	if start > end {
		return Range{}, fmt.Errorf("range start > end: %d-%d", start, end)
	}

	return Range{Start: start, End: end, Length: totalLength}, nil
}

// ContentRange 构造 Content-Range 响应头值。
func ContentRange(r Range) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.Start, r.End, r.Length)
}

// IsSatisfiable 判断 Range 请求是否可满足。
func IsSatisfiable(r Range) bool {
	return r.Start < r.Length
}

// IsRangeRequest 判断请求是否包含 Range 头。
func IsRangeRequest(req *http.Request) bool {
	return req.Header.Get("Range") != ""
}
