package human

import "fmt"

func Size(n int64) string {
	f := float64(n)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1024 || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%dB", n)
			}
			return fmt.Sprintf("%.1f%s", f, u)
		}
		f /= 1024
	}
	return fmt.Sprintf("%.1fTB", f)
}

func Parse(s string) int64 {
	var v float64
	var u string
	_, _ = fmt.Sscanf(s, "%f%s", &v, &u)
	switch u {
	case "KB", "K":
		return int64(v * 1024)
	case "MB", "M":
		return int64(v * 1024 * 1024)
	case "GB", "G":
		return int64(v * 1024 * 1024 * 1024)
	case "TB", "T":
		return int64(v * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(v)
	}
}
