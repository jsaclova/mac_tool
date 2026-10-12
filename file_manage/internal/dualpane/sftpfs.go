package dualpane

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SftpFS는 SFTP 원격 구현이다.
type SftpFS struct {
	label  string
	client *sftp.Client
	ssh    *ssh.Client
}

func (s *SftpFS) Name() string { return s.label }

func (s *SftpFS) ListDir(dir string) ([]Entry, error) {
	fis, err := s.client.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		out = append(out, Entry{fi.Name(), path.Join(dir, fi.Name()), fi.Size(), fi.ModTime(), fi.IsDir()})
	}
	return out, nil
}

func (s *SftpFS) Mkdir(dir string) error                   { return s.client.MkdirAll(dir) }
func (s *SftpFS) Remove(p string) error                    { return s.client.Remove(p) }
func (s *SftpFS) RemoveAll(p string) error                 { return removeAllRemote(s.client, p) }
func (s *SftpFS) Rename(o, n string) error                 { return s.client.Rename(o, n) }
func (s *SftpFS) Close() error                             { _ = s.client.Close(); return s.ssh.Close() }
func (s *SftpFS) OpenRead(p string) (io.ReadCloser, error) { return s.client.Open(p) }
func (s *SftpFS) OpenWrite(p string, mode os.FileMode) (io.WriteCloser, error) {
	if err := s.client.MkdirAll(path.Dir(p)); err != nil {
		return nil, err
	}
	return s.client.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
}

func removeAllRemote(c *sftp.Client, p string) error {
	fi, err := c.Stat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return c.Remove(p)
	}
	fis, err := c.ReadDir(p)
	if err != nil {
		return err
	}
	for _, f := range fis {
		if err := removeAllRemote(c, path.Join(p, f.Name())); err != nil {
			return err
		}
	}
	return c.RemoveDirectory(p)
}

// Dial은 SSH 연결 후 SFTP 클라이언트를 연다.
// keyAuth 실패 시 password가 비어있지 않으면 비밀번호 인증을 시도한다.
func Dial(a Addr, password string) (*SftpFS, error) {
	var signers []ssh.Signer

	// 1. ssh-agent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			ag := agent.NewClient(conn)
			if sgs, err := ag.Signers(); err == nil {
				signers = append(signers, sgs...)
			}
		}
	}
	// 2. 기본 키 파일
	home, _ := os.UserHomeDir()
	for _, k := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
		key, err := os.ReadFile(home + "/.ssh/" + k)
		if err != nil {
			continue
		}
		if sg, err := ssh.ParsePrivateKey(key); err == nil {
			signers = append(signers, sg)
		}
	}

	kh := hostKeyCallback()
	cfg := &ssh.ClientConfig{
		User:            a.User,
		Auth:            []ssh.AuthMethod{},
		HostKeyCallback: kh,
		Timeout:         15 * time.Second,
	}
	if len(signers) > 0 {
		cfg.Auth = append(cfg.Auth, ssh.PublicKeys(signers...))
	}
	if password != "" {
		cfg.Auth = append(cfg.Auth, ssh.Password(password))
		// 일부 서버(PAM 등)는 password 방식 대신 keyboard-interactive 만 허용한다.
		cfg.Auth = append(cfg.Auth, ssh.KeyboardInteractive(
			func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}))
	}
	if len(cfg.Auth) == 0 {
		return nil, fmt.Errorf("인증 수단 없음 (키/에이전트 없음, 비밀번호 필요)")
	}

	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", a.Host, a.Port), cfg)
	if err != nil {
		return nil, friendlySSHError(err)
	}
	sc, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &SftpFS{label: a.String(), client: sc, ssh: conn}, nil
}

// friendlySSHError는 흔한 SSH 오류를 한국어 안내로 바꾼다.
func friendlySSHError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"),
		strings.Contains(msg, "no supported methods remain"):
		return fmt.Errorf("인증 실패 — 아이디/비밀번호 확인 (또는 키 미등록). 원문: %s", msg)
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("연결 거부 — 호스트/포트 확인. 원문: %s", msg)
	case strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "connection timed out"):
		return fmt.Errorf("연결 시간 초과 — IP/네트워크/방화벽 확인. 원문: %s", msg)
	case strings.Contains(msg, "no such host"):
		return fmt.Errorf("호스트를 찾을 수 없음 — 주소 확인. 원문: %s", msg)
	}
	return err
}

// hostKeyCallback은 known_hosts 검증 + 최초접속 자동 등록(TOFU)이다.
func hostKeyCallback() ssh.HostKeyCallback {
	home, _ := os.UserHomeDir()
	khFile := home + "/.ssh/known_hosts"
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if cb, err := knownhosts.New(khFile); err == nil {
			if err := cb(hostname, remote, key); err == nil {
				return nil // 이미 등록됨
			}
		}
		f, err := os.OpenFile(khFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.WriteString(knownhosts.Line([]string{hostname}, key) + "\n")
		return err
	}
}
