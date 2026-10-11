package dualpane

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Addr는 SSH 접속 주소다.
type Addr struct {
	User string
	Host string
	Port int
	Path string // 접속 후 시작 경로
	Raw  string // 입력 원문 (캐시 키)
}

func (a Addr) String() string {
	if a.Port != 22 {
		return fmt.Sprintf("%s@%s:%d:%s", a.User, a.Host, a.Port, a.Path)
	}
	return fmt.Sprintf("%s@%s:%s", a.User, a.Host, a.Path)
}

// ParseAddr는 "user@host[:port][:/path]", "ssh://[user@]host[:port][/path]" 를 파싱한다.
func ParseAddr(s string) (Addr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Addr{}, fmt.Errorf("빈 주소")
	}
	raw := s
	s = strings.TrimPrefix(s, "ssh://")
	var user, rest string
	if i := strings.LastIndex(s, "@"); i >= 0 {
		user = s[:i]
		rest = s[i+1:]
	} else {
		rest = s
	}
	// "host[:port][/path]" 분리. "host:/path" 의 trailing 콜론 제거
	head, path := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		head, path = rest[:i], rest[i:]
	}
	head = strings.TrimSuffix(head, ":")
	host := head
	port := 22
	if strings.Contains(head, ":") {
		if h, p, err := net.SplitHostPort(head); err == nil {
			host = h
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
	}
	if user == "" {
		if u := os.Getenv("USER"); u != "" {
			user = u
		} else {
			user = "root"
		}
	}
	if path == "" {
		path = "."
	}
	if host == "" {
		return Addr{}, fmt.Errorf("호스트 없음: %s", raw)
	}
	return Addr{User: user, Host: host, Port: port, Path: path, Raw: raw}, nil
}

func hostsFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "fm", "hosts.json")
}

// LoadHosts는 캐시된 접속 주소 목록을 반환한다 (최근순).
func LoadHosts() []string {
	data, err := os.ReadFile(hostsFile())
	if err != nil {
		return nil
	}
	var out []string
	_ = json.Unmarshal(data, &out)
	return out
}

// SaveHost는 접속 주소를 캐시에 저장한다 (중복은 앞으로 이동, 최대 20).
func SaveHost(raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	list := LoadHosts()
	keep := []string{raw}
	for _, h := range list {
		if h != raw && len(keep) < 20 {
			keep = append(keep, h)
		}
	}
	_ = os.MkdirAll(filepath.Dir(hostsFile()), 0o755)
	data, _ := json.MarshalIndent(keep, "", "  ")
	_ = os.WriteFile(hostsFile(), data, 0o600)
}
